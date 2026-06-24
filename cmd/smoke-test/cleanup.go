package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/pipeline"
)

func cleanup(ctx context.Context, gh github.Service, state *pipeline.State, issueNumber int, result string) error {
	if state.PRNumber > 0 {
		if err := gh.ClosePullRequest(ctx, state.PRNumber); err != nil {
			slog.Warn("failed to close PR", "pr", state.PRNumber, "error", err)
		} else {
			slog.Info("closed PR", "pr", state.PRNumber)
		}
	}

	if state.PRBranch != "" {
		if err := gh.DeleteBranch(ctx, state.PRBranch); err != nil {
			slog.Warn("failed to delete branch", "branch", state.PRBranch, "error", err)
		} else {
			slog.Info("deleted branch", "branch", state.PRBranch)
		}
	}

	summary := fmt.Sprintf("## Smoke Test Complete\n\n"+
		"- PR: #%d (closed)\n"+
		"- Files: %d\n"+
		"- Phases: %d\n"+
		"- Result: %s",
		state.PRNumber, len(state.Files), len(state.PhaseTokens), result)

	if err := gh.CreateComment(ctx, issueNumber, summary); err != nil {
		slog.Warn("failed to post summary comment", "error", err)
	}

	if err := gh.CloseIssue(ctx, issueNumber); err != nil {
		return fmt.Errorf("close issue %d: %w", issueNumber, err)
	}
	slog.Info("closed issue", "issue", issueNumber)

	return nil
}
