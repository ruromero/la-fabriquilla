package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/github"
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
	err := orch.reviewWithExternalLoop(context.Background(), key, statePath, 1, 0)
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
	err := orch.reviewWithExternalLoop(context.Background(), key, statePath, 1, 0)
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
	err := orch.reviewWithExternalLoop(context.Background(), key, statePath, 1, 0)
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
	err := orch.reviewWithExternalLoop(context.Background(), key, statePath, 1, 0)
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
	err := orch.reviewWithExternalLoop(context.Background(), key, statePath, 1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (reviewer only, stale arbiter ignored), got %d", calls)
	}
}

func TestReplanLoop_SucceedsOnSecondAttempt(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber:      1,
		IssueTitle:       "test",
		IssueBody:        "test",
		Phase:            "code-done",
		CoderOutcome:     "plan_infeasible",
		InfeasibleReason: "Function Foo does not exist",
		PlanOutcome:      "plan",
		PlanContent:      "original plan",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	gh := &mockGHForReplan{}

	plannerCalls := 0
	designerCalls := 0
	coderCalls := 0

	runner := func(ctx context.Context, cfg *config.Config, binary, sp string, issueNumber int, sandboxImage string) error {
		s, _ := LoadState(sp)
		switch binary {
		case "planner":
			plannerCalls++
			s.PlanOutcome = "plan"
			s.PlanContent = "revised plan"
			s.Phase = "plan-done"
		case "designer":
			designerCalls++
			s.Design = "revised design"
			s.Phase = "design-done"
		case "coder":
			coderCalls++
			s.CoderOutcome = "success"
			s.InfeasibleReason = ""
			s.Code = `{"files":[]}`
			s.Phase = "code-done"
		}
		SaveState(sp, s)
		return nil
	}

	cfg := &config.Config{MaxReplans: 2, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	orch.GH = gh

	err := orch.replanLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plannerCalls != 1 {
		t.Errorf("planner calls = %d, want 1", plannerCalls)
	}
	if designerCalls != 1 {
		t.Errorf("designer calls = %d, want 1", designerCalls)
	}
	if coderCalls != 1 {
		t.Errorf("coder calls = %d, want 1", coderCalls)
	}

	s, _ := store.Load(context.Background(), key)
	if s.ReplanCount != 1 {
		t.Errorf("ReplanCount = %d, want 1", s.ReplanCount)
	}
	if s.CoderOutcome != "success" {
		t.Errorf("CoderOutcome = %q, want %q", s.CoderOutcome, "success")
	}
}

func TestReplanLoop_CapExhausted(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber:      1,
		IssueTitle:       "test",
		IssueBody:        "test",
		Phase:            "code-done",
		CoderOutcome:     "plan_infeasible",
		InfeasibleReason: "Function Foo does not exist",
		PlanOutcome:      "plan",
		PlanContent:      "original plan",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	gh := &mockGHForReplan{}

	runner := func(ctx context.Context, cfg *config.Config, binary, sp string, issueNumber int, sandboxImage string) error {
		s, _ := LoadState(sp)
		switch binary {
		case "planner":
			s.PlanOutcome = "plan"
			s.PlanContent = "another bad plan"
			s.Phase = "plan-done"
		case "designer":
			s.Design = "another bad design"
			s.Phase = "design-done"
		case "coder":
			s.CoderOutcome = "plan_infeasible"
			s.InfeasibleReason = "Still infeasible"
			s.Phase = "code-done"
		}
		SaveState(sp, s)
		return nil
	}

	cfg := &config.Config{MaxReplans: 1, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	orch.GH = gh

	err := orch.replanLoop(context.Background(), key, statePath, 1)
	if err == nil {
		t.Fatal("expected error when replan cap exhausted")
	}

	hasLabel := false
	for _, l := range gh.labels {
		if l == "fabriquilla:needs-human" {
			hasLabel = true
		}
	}
	if !hasLabel {
		t.Error("expected fabriquilla:needs-human label")
	}
	if len(gh.comments) == 0 {
		t.Error("expected escalation comment")
	}
}

func TestReplanLoop_PlannerReturnsNeedsInfo(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber:      1,
		IssueTitle:       "test",
		IssueBody:        "test",
		Phase:            "code-done",
		CoderOutcome:     "plan_infeasible",
		InfeasibleReason: "missing symbols",
		PlanOutcome:      "plan",
		PlanContent:      "original plan",
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	gh := &mockGHForReplan{}

	runner := func(ctx context.Context, cfg *config.Config, binary, sp string, issueNumber int, sandboxImage string) error {
		s, _ := LoadState(sp)
		if binary == "planner" {
			s.PlanOutcome = "needs_info"
			s.PlanContent = "NEEDS_INFO: what database?"
			s.Phase = "plan-done"
		}
		SaveState(sp, s)
		return nil
	}

	cfg := &config.Config{MaxReplans: 2, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	orch.GH = gh

	err := orch.replanLoop(context.Background(), key, statePath, 1)
	if err == nil {
		t.Fatal("expected error when replanner returns needs_info")
	}

	hasLabel := false
	for _, l := range gh.labels {
		if l == "fabriquilla:needs-human" {
			hasLabel = true
		}
	}
	if !hasLabel {
		t.Error("expected fabriquilla:needs-human label")
	}
}

func TestReplanLoop_ClearsStaleState(t *testing.T) {
	dir := t.TempDir()
	state := &State{
		IssueNumber:      1,
		IssueTitle:       "test",
		IssueBody:        "test",
		Phase:            "code-done",
		CoderOutcome:     "plan_infeasible",
		InfeasibleReason: "bad refs",
		PlanOutcome:      "plan",
		PlanContent:      "original plan",
		Design:           "stale design",
		Code:             "stale code",
		Review:           &ReviewState{},
		ArbiterResult:    &ArbiterState{},
		Files:            []FileState{{Path: "stale.go", Content: "stale"}},
	}
	statePath := writeTestState(t, dir, state)

	store := NewFileStateStore(dir)
	key := "state"

	gh := &mockGHForReplan{}

	plannerSawClean := false
	runner := func(ctx context.Context, cfg *config.Config, binary, sp string, issueNumber int, sandboxImage string) error {
		s, _ := LoadState(sp)
		switch binary {
		case "planner":
			if s.PlanOutcome == "" && s.PlanContent == "" && s.Design == "" && s.Code == "" && s.Review == nil && s.ArbiterResult == nil && len(s.Files) == 0 && s.CoderOutcome == "" && s.InfeasibleReason == "" {
				plannerSawClean = true
			}
			s.PlanOutcome = "plan"
			s.PlanContent = "revised plan"
			s.Phase = "plan-done"
		case "designer":
			s.Design = "revised design"
			s.Phase = "design-done"
		case "coder":
			s.CoderOutcome = "success"
			s.Code = `{"files":[]}`
			s.Phase = "code-done"
		}
		SaveState(sp, s)
		return nil
	}

	cfg := &config.Config{MaxReplans: 1, MaxCostBudget: 100000}
	orch := makeOrch(cfg, store, runner, "")
	orch.GH = gh

	err := orch.replanLoop(context.Background(), key, statePath, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plannerSawClean {
		t.Error("planner should see cleaned state (no stale PlanOutcome, PlanContent, Design, Code, Review, Files, CoderOutcome, InfeasibleReason)")
	}
}

type mockGHForReplan struct {
	labels   []string
	comments []string
}

func (m *mockGHForReplan) Owner() string { return "test" }
func (m *mockGHForReplan) Repo() string  { return "test" }
func (m *mockGHForReplan) AddLabel(_ context.Context, _ int, label string) error {
	m.labels = append(m.labels, label)
	return nil
}
func (m *mockGHForReplan) RemoveLabel(_ context.Context, _ int, _ string) error { return nil }
func (m *mockGHForReplan) CreateComment(_ context.Context, _ int, body string) error {
	m.comments = append(m.comments, body)
	return nil
}
func (m *mockGHForReplan) ListComments(_ context.Context, _ int) ([]github.Comment, error) {
	return nil, nil
}
func (m *mockGHForReplan) ListIssuesByLabel(_ context.Context, _ string) ([]github.Issue, error) {
	return nil, nil
}
func (m *mockGHForReplan) CreateIssue(_ context.Context, _, _ string, _ []string) (github.Issue, error) {
	return github.Issue{}, nil
}
func (m *mockGHForReplan) FileExists(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mockGHForReplan) GetFileContent(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockGHForReplan) CheckReadiness(_ context.Context) (github.ReadinessResult, error) {
	return github.ReadinessResult{Ready: true}, nil
}
func (m *mockGHForReplan) CloneShallow(_ context.Context) (string, func(), error) {
	return "", func() {}, nil
}
func (m *mockGHForReplan) GetBranchSHA(_ context.Context, _ string) (string, error) {
	return "abc", nil
}
func (m *mockGHForReplan) CreateBranch(_ context.Context, _, _ string) error { return nil }
func (m *mockGHForReplan) CreateCommit(_ context.Context, _, _ string, _ []github.FileChange) (string, error) {
	return "sha", nil
}
func (m *mockGHForReplan) CreatePullRequest(_ context.Context, _, _, _, _ string) (github.PullRequest, error) {
	return github.PullRequest{}, nil
}
func (m *mockGHForReplan) ListPRReviewComments(_ context.Context, _ int) ([]github.Comment, error) {
	return nil, nil
}
func (m *mockGHForReplan) ListPRReviews(_ context.Context, _ int) ([]github.PRReview, error) {
	return nil, nil
}
func (m *mockGHForReplan) ClosePullRequest(_ context.Context, _ int) error { return nil }
func (m *mockGHForReplan) DeleteBranch(_ context.Context, _ string) error  { return nil }
func (m *mockGHForReplan) ListLabels(_ context.Context, _ int) ([]github.Label, error) {
	return nil, nil
}
func (m *mockGHForReplan) CloseIssue(_ context.Context, _ int) error { return nil }
