package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/review"
)

func writeTestState(t *testing.T, dir string, state *pipeline.State) string {
	t.Helper()
	path := filepath.Join(dir, "state.json")
	if err := pipeline.SaveState(path, state); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReviewIterateLoop_CleanReview(t *testing.T) {
	dir := t.TempDir()
	state := &pipeline.State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := pipeline.NewFileStateStore(dir)
	key := "state"

	calls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		calls++
		s, _ := pipeline.LoadState(statePath)
		if binary == "reviewer" {
			s.Review = &pipeline.ReviewState{
				Findings: []review.ReviewFinding{},
			}
			s.Phase = "review-done"
		}
		pipeline.SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	err := reviewIterateLoop(context.Background(), cfg, store, key, statePath, "", 1, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only), got %d", calls)
	}
}

func TestReviewIterateLoop_MaxIterations(t *testing.T) {
	dir := t.TempDir()
	state := &pipeline.State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := pipeline.NewFileStateStore(dir)
	key := "state"

	reviewerCalls := 0
	iteratorCalls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		s, _ := pipeline.LoadState(statePath)
		switch binary {
		case "reviewer":
			reviewerCalls++
			s.Review = &pipeline.ReviewState{
				Findings: []review.ReviewFinding{
					{Severity: review.SeverityCritical, Title: "Bug — something is wrong"},
				},
			}
			s.Phase = "review-done"
		case "iterator":
			iteratorCalls++
			s.Phase = "iterate-done"
		}
		pipeline.SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	err := reviewIterateLoop(context.Background(), cfg, store, key, statePath, "", 1, runner)
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
	state := &pipeline.State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := pipeline.NewFileStateStore(dir)
	key := "state"

	reviewerCalls := 0
	iteratorCalls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		s, _ := pipeline.LoadState(statePath)
		switch binary {
		case "reviewer":
			reviewerCalls++
			if reviewerCalls == 1 {
				s.Review = &pipeline.ReviewState{
					Findings: []review.ReviewFinding{
						{Severity: review.SeverityCritical, Title: "Bug — something is wrong"},
					},
				}
			} else {
				s.Review = &pipeline.ReviewState{
					Findings: []review.ReviewFinding{},
				}
			}
			s.Phase = "review-done"
		case "iterator":
			iteratorCalls++
			s.Phase = "iterate-done"
		}
		pipeline.SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	err := reviewIterateLoop(context.Background(), cfg, store, key, statePath, "", 1, runner)
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
	state := &pipeline.State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
	}
	statePath := writeTestState(t, dir, state)

	store := pipeline.NewFileStateStore(dir)
	key := "state"

	calls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		calls++
		s, _ := pipeline.LoadState(statePath)
		if binary == "reviewer" {
			s.Review = &pipeline.ReviewState{
				Findings: []review.ReviewFinding{
					{Severity: review.SeverityCritical, Title: "false positive"},
				},
			}
			s.ArbiterResult = &pipeline.ArbiterState{
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
		pipeline.SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{
		MaxIterations: 3,
		MaxCostBudget: 100000,
		Arbiter:       config.ArbiterConfig{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
	}
	err := reviewIterateLoop(context.Background(), cfg, store, key, statePath, "", 1, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only, arbiter dismissed all), got %d", calls)
	}
}

func TestReviewIterateLoop_StaleArbiterIgnoredWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	state := &pipeline.State{
		IssueNumber: 1,
		Code:        "package main",
		Phase:       "commit-done",
		ArbiterResult: &pipeline.ArbiterState{
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

	store := pipeline.NewFileStateStore(dir)
	key := "state"

	calls := 0
	runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
		calls++
		s, _ := pipeline.LoadState(statePath)
		if binary == "reviewer" {
			s.Review = &pipeline.ReviewState{
				Findings: []review.ReviewFinding{},
			}
			s.ArbiterResult = nil
			s.Phase = "review-done"
		}
		pipeline.SaveState(statePath, s)
		return nil
	}

	cfg := &config.Config{MaxIterations: 3, MaxCostBudget: 100000}
	err := reviewIterateLoop(context.Background(), cfg, store, key, statePath, "", 1, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only, stale arbiter ignored), got %d", calls)
	}
}
