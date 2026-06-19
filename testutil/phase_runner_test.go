package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruromero/la-fabriquilla/pipeline"
)

func TestCannedPhaseRunner(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	state := &pipeline.State{
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 1,
		Phase:       "init",
		IssueTitle:  "Add hello function",
		IssueBody:   "Add a hello() function to main.go",
	}
	if err := pipeline.SaveState(statePath, state); err != nil {
		t.Fatal(err)
	}

	runner := NewCannedPhaseRunner()
	ctx := context.Background()

	if err := runner.Run(ctx, "gatherer", statePath); err != nil {
		t.Fatalf("gatherer failed: %v", err)
	}

	state, err := pipeline.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "gathered" {
		t.Errorf("phase = %q, want %q", state.Phase, "gathered")
	}
	if state.GatheredContext == "" {
		t.Error("gathered_context should be populated")
	}
}

func TestCannedPhaseRunnerFullPipeline(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	state := &pipeline.State{
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 1,
		Phase:       "init",
		IssueTitle:  "Add hello function",
		IssueBody:   "Add a hello() function to main.go",
	}
	if err := pipeline.SaveState(statePath, state); err != nil {
		t.Fatal(err)
	}

	runner := NewCannedPhaseRunner()
	ctx := context.Background()

	phases := []string{"gatherer", "researcher", "planner", "designer", "coder", "committer", "reviewer"}
	for _, phase := range phases {
		if err := runner.Run(ctx, phase, statePath); err != nil {
			t.Fatalf("%s failed: %v", phase, err)
		}
	}

	state, err := pipeline.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.PRNumber == 0 {
		t.Error("expected PR number to be set after committer")
	}
	if len(state.Files) == 0 {
		t.Error("expected files to be populated after coder")
	}
	if state.Phase != "reviewed" {
		t.Errorf("phase = %q, want %q", state.Phase, "reviewed")
	}

	_ = os.Remove(statePath)
}
