package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/github"
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
	tools, handler := harness.BuildCoderTools(serenaClient)

	if state.Review == nil {
		slog.Error("no review findings in state")
		os.Exit(1)
	}
	feedback := pipeline.FormatReviewFeedback(state.Review.Correctness, state.Review.Security, state.Review.Intent)

	start := time.Now()
	iterResult, err := agents.IterateWithUsage(ctx, cl, state.Code, feedback, tools, handler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("iterate phase failed", "error", err)
		os.Exit(1)
	}

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

	parsed, err := pipeline.ParseStructuredCodeOutput(iterResult.Content)
	if err != nil {
		slog.Info("structured parse failed, falling back to regex", "error", err)
		parsed = pipeline.ParseCodeOutput(iterResult.Content)
	}

	if state.PRBranch != "" {
		gh := helpers.MustGitHubClientForApp(cfg, "committer", state)
		files := make([]github.FileChange, len(parsed))
		for i, f := range parsed {
			files[i] = github.FileChange{Path: f.Path, Content: f.Content}
		}
		commitMsg := fmt.Sprintf("fix: address review findings (#%d, iteration %d)", state.IssueNumber, state.Iteration+1)
		if _, err := gh.CreateCommit(ctx, state.PRBranch, commitMsg, files); err != nil {
			slog.Error("failed to push fixup commit", "error", err)
			os.Exit(1)
		}
		slog.Info("pushed fixup commit", "branch", state.PRBranch, "iteration", state.Iteration+1)
	}

	state.Code = iterResult.Content
	state.Files = parsed
	state.Iteration++
	state.Phase = "iterate-done"
	helpers.MustSaveState(state)
}
