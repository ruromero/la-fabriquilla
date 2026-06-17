package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/inference"
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

	start := time.Now()
	result, err := agents.DesignWithUsage(ctx, cl, model, state.PlanContent, state.ResearchContext, state.Conventions)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("design phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("designer", result.Model, result.PromptTokens, result.CompTokens, 0, elapsed.Seconds())

	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "designer",
		Model:           result.Model,
		PromptTokens:    result.PromptTokens,
		CompTokens:      result.CompTokens,
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
