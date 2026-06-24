package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/config"
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

func runFull(ctx context.Context, cfgPath string) error {
	cfgVal, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfgVal.Repos) == 0 {
		return fmt.Errorf("no repos configured")
	}

	repo := cfgVal.Repos[0]
	gh, err := helpers.NewGitHubClientForApp(cfgVal, "dispatcher", repo.Owner, repo.Repo)
	if err != nil {
		return fmt.Errorf("create github client: %w", err)
	}

	// Create a fresh issue for this run
	timestamp := time.Now().Format(time.RFC3339)
	issue, err := gh.CreateIssue(ctx,
		fmt.Sprintf("[smoke] Add subtract function — %s", timestamp),
		"Add a `subtract(a, b int) int` function to `main.go` and a corresponding test in `main_test.go`.",
		[]string{"fabriquilla:ready"},
	)
	if err != nil {
		return fmt.Errorf("create test issue: %w", err)
	}
	slog.Info("created test issue", "number", issue.Number)

	stateDir, err := os.MkdirTemp("", "smoke-state-*")
	if err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	defer os.RemoveAll(stateDir)

	cfgVal.StateDir = stateDir

	sandboxImage := ""
	if repo.SandboxImage != "" {
		sandboxImage = repo.SandboxImage
	} else if repo.Language != "" {
		sandboxImage = "factory-" + repo.Language + ":latest"
	}

	orch := &pipeline.Orchestrator{
		GH:           gh,
		Config:       &cfgVal,
		Store:        pipeline.NewFileStateStore(cfgVal.StateDir),
		RunPhase:     runPhaseSubprocess(cfgPath),
		SandboxImage: sandboxImage,
		ConfigPath:   cfgPath,
	}

	state, err := orch.ProcessIssue(ctx, issue)

	result := "PASSED"
	if err != nil {
		result = fmt.Sprintf("FAILED: %v", err)
	}

	// Guard against nil state if ProcessIssue failed before creating state
	if state == nil {
		state = &pipeline.State{}
	}

	// Always attempt cleanup, even if processing failed
	cleanupErr := cleanup(ctx, gh, state, issue.Number, result)

	if err != nil {
		return err
	}
	return cleanupErr
}

func runPhaseSubprocess(cfgPath string) pipeline.PhaseRunner {
	return func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		timeout := cfg.PhaseDuration(binary)
		phaseCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(phaseCtx, binary)
		cmd.Env = append(os.Environ(),
			"PIPELINE_STATE_PATH="+statePath,
			"CONFIG_PATH="+cfgPath,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		slog.Info("running phase", "phase", binary)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", binary, err)
		}
		return nil
	}
}
