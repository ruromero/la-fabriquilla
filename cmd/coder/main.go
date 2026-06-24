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
	"github.com/ruromero/la-fabriquilla/traces"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()
	model, baseURL, apiKey, err := cfg.ResolveModel(cfg.DefaultModel)
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
	coderTools, coderHandler := harness.BuildCoderTools(serenaClient)

	gh := helpers.MustGitHubClientForApp(cfg, "worker", state)
	rc := harness.LoadRepoContext(ctx, gh, state.AgentInstructionsFile)
	gatherTools, gatherHandler := harness.BuildGatherTools(rc, gh, serenaClient)

	start := time.Now()
	codeResult, err := agents.CodeWithUsage(ctx, cl, model, state.Design, state.ResearchContext, state.Conventions, coderTools, coderHandler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("code phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("coder", codeResult.Model, codeResult.PromptTokens, codeResult.CompTokens, codeResult.ToolCalls, elapsed.Seconds())
	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "coder",
		Model:           codeResult.Model,
		PromptTokens:    codeResult.PromptTokens,
		CompTokens:      codeResult.CompTokens,
		ToolCalls:       codeResult.ToolCalls,
		Duration:        elapsed.String(),
		StartedAt:       start,
		CumPromptTokens: state.TotalPromptTokens,
		CumCompTokens:   state.TotalCompTokens,
		CumCostUSD:      state.TotalCostUSD,
	})

	code := codeResult.Content

	start = time.Now()
	review, err := agents.Review(ctx, cl, code, state.Design, state.PlanContent, state.Conventions, gatherTools, gatherHandler)
	elapsed = time.Since(start)
	if err != nil {
		slog.Error("review phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("reviewer", review.Model, review.PromptTokens, review.CompTokens, review.ToolCalls, elapsed.Seconds())
	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "reviewer",
		Model:           review.Model,
		PromptTokens:    review.PromptTokens,
		CompTokens:      review.CompTokens,
		ToolCalls:       review.ToolCalls,
		Duration:        elapsed.String(),
		StartedAt:       start,
		CumPromptTokens: state.TotalPromptTokens,
		CumCompTokens:   state.TotalCompTokens,
		CumCostUSD:      state.TotalCostUSD,
	})

	for i := 0; i < cfg.MaxIterations && pipeline.ReviewNeedsIteration(review.Findings); i++ {
		slog.Info("starting iteration", "iteration", i+1, "max", cfg.MaxIterations)
		feedback := pipeline.FormatReviewFeedback(review.Findings)

		start = time.Now()
		iterResult, err := agents.IterateWithUsage(ctx, cl, model, code, feedback, coderTools, coderHandler)
		elapsed = time.Since(start)
		if err != nil {
			slog.Error("iterate phase failed", "iteration", i+1, "error", err)
			os.Exit(1)
		}
		code = iterResult.Content

		state.RecordTokenUsage("iterator", iterResult.Model, iterResult.PromptTokens, iterResult.CompTokens, iterResult.ToolCalls, elapsed.Seconds())
		traces.Log(traces.Trace{
			IssueNumber:     state.IssueNumber,
			Phase:           "iterator",
			Model:           iterResult.Model,
			PromptTokens:    iterResult.PromptTokens,
			CompTokens:      iterResult.CompTokens,
			ToolCalls:       iterResult.ToolCalls,
			Duration:        elapsed.String(),
			StartedAt:       start,
			CumPromptTokens: state.TotalPromptTokens,
			CumCompTokens:   state.TotalCompTokens,
			CumCostUSD:      state.TotalCostUSD,
		})

		start = time.Now()
		review, err = agents.Review(ctx, cl, code, state.Design, state.PlanContent, state.Conventions, gatherTools, gatherHandler)
		elapsed = time.Since(start)
		if err != nil {
			slog.Error("review phase failed", "iteration", i+1, "error", err)
			os.Exit(1)
		}

		state.RecordTokenUsage("reviewer", review.Model, review.PromptTokens, review.CompTokens, review.ToolCalls, elapsed.Seconds())
		traces.Log(traces.Trace{
			IssueNumber:     state.IssueNumber,
			Phase:           "reviewer",
			Model:           review.Model,
			PromptTokens:    review.PromptTokens,
			CompTokens:      review.CompTokens,
			ToolCalls:       review.ToolCalls,
			Duration:        elapsed.String(),
			StartedAt:       start,
			CumPromptTokens: state.TotalPromptTokens,
			CumCompTokens:   state.TotalCompTokens,
			CumCostUSD:      state.TotalCostUSD,
		})
	}

	parsed, err := pipeline.ParseStructuredCodeOutput(code)
	if err != nil {
		slog.Info("structured parse failed, falling back to regex", "error", err)
		parsed = pipeline.ParseCodeOutput(code)
	}

	state.Code = code
	state.Review = &pipeline.ReviewState{
		Findings: review.Findings,
	}
	state.Files = parsed
	state.Phase = "code-done"
	helpers.MustSaveState(state)
}
