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
	model, baseURL, apiKey, err := cfg.ResolveModel(cfg.ModelFor("designer"))
	if err != nil {
		slog.Error("resolve default model", "error", err)
		os.Exit(1)
	}

	cl := inference.NewClient(baseURL, inference.WithAPIKey(apiKey))
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

	rc := harness.LoadRepoContext(ctx, gh, state.IncludeDocs)

	var serenaClient *mcp.Client
	if sess != nil {
		serenaClient = sess.Client
	}
	tools, handler := harness.BuildDesignerTools(rc, gh, serenaClient)

	start := time.Now()
	result, err := agents.DesignWithUsage(ctx, cl, model, state.PlanContent, state.ResearchContext, state.Conventions, tools, handler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("design phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("designer", result.Model, result.PromptTokens, result.CompTokens, result.ToolCalls, elapsed.Seconds())

	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "designer",
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

	state.Design = result.Content
	state.Phase = "design-done"
	helpers.MustSaveState(state)
}
