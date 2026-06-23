package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/testutil"
)

func runFullMock(ctx context.Context) error {
	stateDir, err := os.MkdirTemp("", "smoke-state-*")
	if err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	defer os.RemoveAll(stateDir)

	gh := testutil.NewMemoryClient("smoke-owner", "smoke-repo",
		testutil.WithIssue(github.Issue{
			Number: 1,
			Title:  "Add greeting function",
			Body:   "Add a Greet(name string) function that returns \"Hello, <name>!\"",
			Labels: []github.Label{{Name: "fabriquilla:ready"}},
		}),
		testutil.WithFile("CODEOWNERS", "* @smoke-owner"),
		testutil.WithFile("CLAUDE.md", "# Test repo"),
		testutil.WithFile("CONVENTIONS.md", "# Conventions"),
		testutil.WithFile("ARCHITECTURE.md", "# Architecture"),
		testutil.WithFile("README.md", "# Test"),
		testutil.WithFile(".serena", ""),
	)

	runner := testutil.NewCannedPhaseRunner()
	store := pipeline.NewFileStateStore(stateDir)
	key := pipeline.StateKey("smoke-owner", "smoke-repo", 1)

	state := &pipeline.State{
		RepoOwner:   "smoke-owner",
		RepoName:    "smoke-repo",
		IssueNumber: 1,
		Phase:       "init",
		IssueTitle:  "Add greeting function",
		IssueBody:   "Add a Greet(name string) function that returns \"Hello, <name>!\"",
		StartedAt:   time.Now(),
	}

	if err := store.Save(ctx, key, state); err != nil {
		return fmt.Errorf("save initial state: %w", err)
	}
	statePath := store.StatePath(key)

	phases := []string{"gatherer", "researcher", "planner", "designer", "coder", "committer", "reviewer"}
	for _, phase := range phases {
		if err := runner.Run(ctx, phase, statePath); err != nil {
			return fmt.Errorf("phase %s: %w", phase, err)
		}
	}

	state, err = store.Load(ctx, key)
	if err != nil {
		return fmt.Errorf("load final state: %w", err)
	}

	// Simulate the dispatcher's GitHub interactions after the pipeline completes,
	// so the verifier has recorded metadata to scan for credential leakage.
	if state.PRNumber != 0 {
		prBody := fmt.Sprintf("Automated PR for issue #%d\n\nFiles changed: %d", state.IssueNumber, len(state.Files))
		if _, err := gh.CreatePullRequest(ctx, state.IssueTitle, prBody, state.PRBranch, "main"); err != nil {
			return fmt.Errorf("simulate PR creation: %w", err)
		}
		if err := gh.CreateComment(ctx, state.IssueNumber, fmt.Sprintf("PR #%d created for this issue.", state.PRNumber)); err != nil {
			return fmt.Errorf("simulate comment: %w", err)
		}
	}

	return verify(state, gh)
}

func runMockGitHub(ctx context.Context, configPath string) error {
	return fmt.Errorf("mock-github mode requires Ollama; not yet implemented")
}

func runFull(ctx context.Context, configPath string) error {
	return fmt.Errorf("full mode requires real GitHub test repo; not yet implemented")
}
