package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/harness"
	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/mcp"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/review"
	"github.com/ruromero/la-fabriquilla/traces"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	_, baseURL, apiKey, err := cfg.ResolveModel(cfg.DefaultModel)
	if err != nil {
		slog.Error("resolve default model", "error", err)
		os.Exit(1)
	}
	cl := inference.NewClient(baseURL, inference.WithAPIKey(apiKey))
	ctx := context.Background()

	var sess *harness.SerenaSession
	if state.CloneDir != "" {
		var err error
		sess, err = harness.StartSerenaFromClone(ctx, state.CloneDir, cfg.Serena)
		if err != nil {
			slog.Warn("failed to start Serena", "error", err)
		}
	}
	if sess != nil {
		defer sess.Cleanup()
	}

	var serenaClient *mcp.Client
	if sess != nil {
		serenaClient = sess.Client
	}

	gh := helpers.MustGitHubClientForApp(cfg, "worker", state)
	rc := harness.LoadRepoContext(ctx, gh)
	tools, handler := harness.BuildGatherTools(rc, gh, serenaClient)

	start := time.Now()
	rev, err := agents.Review(ctx, cl, state.Code, state.Design, state.PlanContent, state.Conventions, tools, handler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("review phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("reviewer", rev.Model, rev.PromptTokens, rev.CompTokens, rev.ToolCalls, elapsed.Seconds())
	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "reviewer",
		Model:           rev.Model,
		PromptTokens:    rev.PromptTokens,
		CompTokens:      rev.CompTokens,
		ToolCalls:       rev.ToolCalls,
		Duration:        elapsed.String(),
		StartedAt:       start,
		CumPromptTokens: state.TotalPromptTokens,
		CumCompTokens:   state.TotalCompTokens,
		CumCostUSD:      state.TotalCostUSD,
	})

	state.Review = &pipeline.ReviewState{
		Findings: rev.Findings,
	}

	if cfg.Arbiter.Model == "" {
		state.ArbiterResult = nil
	}

	if cfg.Arbiter.Model != "" {
		arbModel, arbURL, arbKey, arbErr := cfg.ResolveModel(cfg.Arbiter.Model)
		if arbErr != nil {
			slog.Error("resolve arbiter model", "error", arbErr)
			os.Exit(1)
		}
		arbCl := inference.NewClient(arbURL, inference.WithAPIKey(arbKey))

		var dismissedKeys []string
		if state.ArbiterResult != nil {
			dismissedKeys = state.ArbiterResult.DismissedKeys
		}

		arbStart := time.Now()
		arb, arbErr2 := agents.Arbitrate(ctx, arbCl, arbModel,
			rev.Findings, state.Conventions, state.Summaries, state.PlanContent,
			dismissedKeys)
		arbElapsed := time.Since(arbStart)
		if arbErr2 != nil {
			slog.Warn("arbiter phase failed, falling back to severity-based review", "error", arbErr2)
			state.ArbiterResult = nil
			state.Phase = "review-done"
			helpers.MustSaveState(state)
			return
		}

		state.RecordTokenUsage("arbiter", arb.Model, arb.PromptTokens, arb.CompTokens, 0, arbElapsed.Seconds())
		traces.Log(traces.Trace{
			IssueNumber:     state.IssueNumber,
			Phase:           "arbiter",
			Model:           arb.Model,
			PromptTokens:    arb.PromptTokens,
			CompTokens:      arb.CompTokens,
			Duration:        arbElapsed.String(),
			StartedAt:       arbStart,
			CumPromptTokens: state.TotalPromptTokens,
			CumCompTokens:   state.TotalCompTokens,
			CumCostUSD:      state.TotalCostUSD,
		})

		var newDismissed []string
		newDismissed = append(newDismissed, dismissedKeys...)
		for _, f := range arb.Result.Findings {
			if f.Classification == review.ClassDismissed {
				newDismissed = append(newDismissed, review.DismissKey(f.Finding))
			}
		}
		state.ArbiterResult = &pipeline.ArbiterState{
			Findings:      arb.Result.Findings,
			DismissedKeys: newDismissed,
		}

		slog.Info("arbiter completed",
			"total_findings", len(arb.Result.Findings),
			"fix_here", countClassification(arb.Result.Findings, review.ClassFixHere),
			"subtask", countClassification(arb.Result.Findings, review.ClassSubtask),
			"root_cause", countClassification(arb.Result.Findings, review.ClassRootCause),
			"dismissed", countClassification(arb.Result.Findings, review.ClassDismissed),
		)
	}

	state.Phase = "review-done"
	helpers.MustSaveState(state)
}

func countClassification(findings []review.ArbiterFinding, c review.Classification) int {
	n := 0
	for _, f := range findings {
		if f.Classification == c {
			n++
		}
	}
	return n
}
