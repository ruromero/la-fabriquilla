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

	cl := inference.NewClient(cfg.Inference.BaseURL, inference.WithAPIKey(cfg.Inference.APIKey))
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

	if !cfg.Arbiter.Enabled() {
		state.ArbiterResult = nil
	}

	if cfg.Arbiter.Enabled() {
		arbCl := inference.NewClient(cfg.Arbiter.BaseURL, inference.WithAPIKey(cfg.Arbiter.APIKey))

		var dismissedTitles []string
		if state.ArbiterResult != nil {
			dismissedTitles = state.ArbiterResult.DismissedTitles
		}

		arbStart := time.Now()
		arb, arbErr := agents.Arbitrate(ctx, arbCl, cfg.Arbiter.Model,
			rev.Findings, state.Conventions, state.Summaries, state.PlanContent,
			dismissedTitles)
		arbElapsed := time.Since(arbStart)
		if arbErr != nil {
			slog.Warn("arbiter phase failed, falling back to severity-based review", "error", arbErr)
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
		newDismissed = append(newDismissed, dismissedTitles...)
		for _, f := range arb.Result.Findings {
			if f.Classification == review.ClassDismissed {
				newDismissed = append(newDismissed, f.Finding.Title)
			}
		}
		state.ArbiterResult = &pipeline.ArbiterState{
			Findings:        arb.Result.Findings,
			DismissedTitles: newDismissed,
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
