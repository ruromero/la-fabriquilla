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
	"github.com/ruromero/la-fabriquilla/traces"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	cl := inference.NewClient(cfg.Inference.BaseURL, inference.WithAPIKey(cfg.Inference.APIKey))
	gh := helpers.MustGitHubClientForApp(cfg, "worker", state)

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

	rc := harness.LoadRepoContext(ctx, gh)

	var serenaClient *mcp.Client
	if sess != nil {
		serenaClient = sess.Client
	}
	tools, handler := harness.BuildGatherTools(rc, gh, serenaClient)

	start := time.Now()
	result, err := agents.GatherContextWithUsage(ctx, cl, state.IssueTitle, state.IssueBody, state.Summaries, tools, handler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("gather context failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("gatherer", result.Model, result.PromptTokens, result.CompTokens, result.ToolCalls, elapsed.Seconds())

	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "gatherer",
		Model:           result.Model,
		PromptTokens:    result.PromptTokens,
		CompTokens:      result.CompTokens,
		ToolCalls:       result.ToolCalls,
		Duration:        elapsed.String(),
		StartedAt:       start,
		CumPromptTokens: state.TotalPromptTokens,
		CumCompTokens:   state.TotalCompTokens,
		CumCostUSD:      state.TotalCostUSD,
	})

	state.GatheredContext = result.Content
	state.Phase = "gather-done"
	helpers.MustSaveState(state)
}
