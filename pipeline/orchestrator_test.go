package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/review"
)

func TestOrchestratorType(t *testing.T) {
	// Verify the type exists and has the expected fields
	o := Orchestrator{}
	_ = o.Config
	_ = o.Store
	_ = o.SandboxImage
	_ = o.RunPhase
}

func writeTestState(t *testing.T, dir string, state *State) string {
	t.Helper()
	path := filepath.Join(dir, "state.json")
	if err := SaveState(path, state); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeOrch returns an Orchestrator wired up for the review-iterate loop tests.
// The runner is injected via RunPhase; Store and Config are set up with the
// temp dir and a permissive budget.
func makeOrch(cfg *config.Config, store *FileStateStore, runner PhaseRunner, sandboxImage string) *Orchestrator {
	return &Orchestrator{
		Config:       cfg,
		Store:        store,
		RunPhase:     runner,
		SandboxImage: sandboxImage,
	}
}

func TestReviewIterateLoop_CleanReview(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	calls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		calls++
		s, _ := LoadState(statePath)
		if binary == "reviewer" {
			s.Review = &ReviewState{
				Findings: []review.ReviewFinding{},
			}
			s.Phase = "review-done"
		}
		SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	err := orch.reviewIterateLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only), got %d", calls)
	}
}

func TestReviewIterateLoop_MaxIterations(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	reviewerCalls := 0
	iteratorCalls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		s, _ := LoadState(statePath)
		switch binary {
		case "reviewer":
			reviewerCalls++
			s.Review = &ReviewState{
				Findings: []review.ReviewFinding{
					{Severity: review.SeverityCritical, Title: "Bug — something is wrong"},
				},
			}
			s.Phase = "review-done"
		case "iterator":
			iteratorCalls++
			s.Phase = "iterate-done"
		}
		SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	err := orch.reviewIterateLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reviewerCalls != 3 {
		t.Errorf("expected 3 reviewer calls, got %d", reviewerCalls)
	}
	if iteratorCalls != 3 {
		t.Errorf("expected 3 iterator calls, got %d", iteratorCalls)
	}
}

func TestReviewIterateLoop_ConvergesMidLoop(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	reviewerCalls := 0
	iteratorCalls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		s, _ := LoadState(statePath)
		switch binary {
		case "reviewer":
			reviewerCalls++
			if reviewerCalls == 1 {
				s.Review = &ReviewState{
					Findings: []review.ReviewFinding{
						{Severity: review.SeverityCritical, Title: "Bug — something is wrong"},
					},
				}
			} else {
				s.Review = &ReviewState{
					Findings: []review.ReviewFinding{},
				}
			}
			s.Phase = "review-done"
		case "iterator":
			iteratorCalls++
			s.Phase = "iterate-done"
		}
		SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	err := orch.reviewIterateLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reviewerCalls != 2 {
		t.Errorf("expected 2 reviewer calls, got %d", reviewerCalls)
	}
	if iteratorCalls != 1 {
		t.Errorf("expected 1 iterator call, got %d", iteratorCalls)
	}
}

func TestReviewIterateLoop_ArbiterDismissesAll(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	calls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		calls++
		s, _ := LoadState(statePath)
		if binary == "reviewer" {
			s.Review = &ReviewState{
				Findings: []review.ReviewFinding{
					{Severity: review.SeverityCritical, Title: "false positive"},
				},
			}
			s.ArbiterResult = &ArbiterState{
				Findings: []review.ArbiterFinding{
					{
						Finding:        review.ReviewFinding{Severity: review.SeverityCritical, Title: "false positive"},
						Classification: review.ClassDismissed,
						Reason:         "invalid per conventions",
					},
				},
			}
			s.Phase = "review-done"
		}
		SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{
		MaxIterations: 3,
		MaxCostBudget: 100000,
		Arbiter:       config.RoleConfig{Model: "deepseek-chat@deepseek"},
		Endpoints: map[string]config.EndpointConfig{
			"deepseek": {BaseURL: "https://api.deepseek.com/v1"},
		},
	}
	orch := makeOrch(cfg, store, runner, "")
	err := orch.reviewIterateLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only, arbiter dismissed all), got %d", calls)
	}
}

func TestReviewIterateLoop_StaleArbiterIgnoredWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
		ArbiterResult: &ArbiterState{
			Findings: []review.ArbiterFinding{
				{
					Finding:        review.ReviewFinding{Severity: review.SeverityCritical, Title: "stale finding"},
					Classification: review.ClassFixHere,
					Reason:         "from a previous run",
				},
			},
		},
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	calls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		calls++
		s, _ := LoadState(statePath)
		if binary == "reviewer" {
			s.Review = &ReviewState{
				Findings: []review.ReviewFinding{},
			}
			s.ArbiterResult = nil
			s.Phase = "review-done"
		}
		SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	err := orch.reviewIterateLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only, stale arbiter ignored), got %d", calls)
	}
}
