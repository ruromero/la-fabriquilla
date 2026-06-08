package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/pipeline"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	gh := helpers.MustGitHubClientForApp(cfg, "committer", state)
	ctx := context.Background()

	if len(state.Files) == 0 {
		slog.Error("no files to commit")
		os.Exit(1)
	}

	files := make([]github.FileChange, len(state.Files))
	for i, f := range state.Files {
		files[i] = github.FileChange{Path: f.Path, Content: f.Content}
	}

	if cfg.ShadowMode {
		slog.Info("shadow mode: posting code as comment", "files", len(files))
		comment := fmt.Sprintf("## Factory: Implementation (Shadow Mode)\n\n%d file(s) generated.\n\n%s", len(files), state.Code)
		gh.CreateComment(ctx, state.IssueNumber, comment)
		gh.RemoveLabel(ctx, state.IssueNumber, "fabriquilla:in-progress")
		gh.AddLabel(ctx, state.IssueNumber, "fabriquilla:done")
		state.Phase = "commit-done"
		helpers.MustSaveState(state)
		return
	}

	branchName := fmt.Sprintf("factory/issue-%d", state.IssueNumber)
	baseSHA, err := gh.GetBranchSHA(ctx, "main")
	if err != nil {
		slog.Error("failed to get base branch SHA", "error", err)
		os.Exit(1)
	}
	if err := gh.CreateBranch(ctx, branchName, baseSHA); err != nil {
		slog.Error("failed to create branch", "error", err)
		os.Exit(1)
	}

	commitMsg := fmt.Sprintf("feat: %s (#%d)", state.IssueTitle, state.IssueNumber)
	if _, err := gh.CreateCommit(ctx, branchName, commitMsg, files); err != nil {
		slog.Error("failed to create commit", "error", err)
		os.Exit(1)
	}

	reviewSummary := ""
	if effective := pipeline.EffectiveFindings(state.ArbiterResult, state.Review); len(effective) > 0 {
		reviewSummary = fmt.Sprintf("\n\n## Review\n\n%s", pipeline.FormatReviewFeedback(effective))
	}

	prTitle := fmt.Sprintf("feat: %s", state.IssueTitle)
	prBody := fmt.Sprintf("Closes #%d\n\n## Plan\n\n%s%s",
		state.IssueNumber, state.PlanContent, reviewSummary)
	pr, err := gh.CreatePullRequest(ctx, prTitle, prBody, branchName, "main")
	if err != nil {
		slog.Error("failed to create PR", "error", err)
		os.Exit(1)
	}

	slog.Info("PR created", "pr", pr.Number, "url", pr.HTMLURL)
	gh.RemoveLabel(ctx, state.IssueNumber, "fabriquilla:in-progress")
	gh.AddLabel(ctx, state.IssueNumber, "fabriquilla:done")

	state.PRNumber = pr.Number
	state.PRBranch = branchName
	state.Phase = "commit-done"
	helpers.MustSaveState(state)
}
