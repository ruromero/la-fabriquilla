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
	review, err := agents.Review(ctx, cl, state.Code, state.Design, state.PlanContent, state.Conventions, tools, handler)
	elapsed := time.Since(start)
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

	state.Review = &pipeline.ReviewState{
		Correctness: review.Correctness,
		Security:    review.Security,
		Intent:      review.Intent,
	}
	state.Phase = "review-done"
	helpers.MustSaveState(state)
}
