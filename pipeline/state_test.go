package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruromero/la-fabriquilla/review"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &State{
		RepoOwner:       "ruromero",
		RepoName:        "la-fabriquilla",
		IssueNumber:     42,
		Phase:           "plan",
		Iteration:       1,
		IssueTitle:      "Add rate limiting",
		IssueBody:       "We need rate limiting on all endpoints",
		GatheredContext: "found middleware package",
		PlanOutcome:     "plan",
		PlanContent:     "Add middleware with token bucket",
		Review: &ReviewState{
			Findings: []review.ReviewFinding{},
		},
		Files: []FileState{
			{Path: "middleware/ratelimit.go", Content: "package middleware"},
		},
		StartedAt: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}

	if err := SaveState(path, original); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.RepoOwner != original.RepoOwner {
		t.Errorf("RepoOwner = %q, want %q", loaded.RepoOwner, original.RepoOwner)
	}
	if loaded.IssueNumber != original.IssueNumber {
		t.Errorf("IssueNumber = %d, want %d", loaded.IssueNumber, original.IssueNumber)
	}
	if loaded.Phase != original.Phase {
		t.Errorf("Phase = %q, want %q", loaded.Phase, original.Phase)
	}
	if loaded.PlanOutcome != original.PlanOutcome {
		t.Errorf("PlanOutcome = %q, want %q", loaded.PlanOutcome, original.PlanOutcome)
	}
	if loaded.Review == nil {
		t.Fatal("Review is nil after round-trip")
	}
	if len(loaded.Review.Findings) != len(original.Review.Findings) {
		t.Errorf("Review.Findings count = %d, want %d", len(loaded.Review.Findings), len(original.Review.Findings))
	}
	if len(loaded.Files) != 1 {
		t.Fatalf("Files count = %d, want 1", len(loaded.Files))
	}
	if loaded.Files[0].Path != "middleware/ratelimit.go" {
		t.Errorf("Files[0].Path = %q, want %q", loaded.Files[0].Path, "middleware/ratelimit.go")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set by SaveState")
	}
}

func TestPhaseTokensRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &State{
		RepoOwner:   "ruromero",
		RepoName:    "la-fabriquilla",
		IssueNumber: 50,
		Phase:       "code-done",
		PhaseTokens: []TokenUsage{
			{
				Phase:            "researcher",
				Model:            "gemini-2.5-flash",
				PromptTokens:     1000,
				CompletionTokens: 500,
				TotalTokens:      1500,
				EstimatedCostUSD: EstimateCost("gemini-2.5-flash", 1000, 500),
				WallTimeSeconds:  2.5,
			},
			{
				Phase:            "gatherer",
				Model:            "qwen3:14b",
				PromptTokens:     2000,
				CompletionTokens: 800,
				TotalTokens:      2800,
				WallTimeSeconds:  15.3,
				ToolCalls:        12,
			},
			{
				Phase:            "coder",
				Model:            "qwen3:14b",
				PromptTokens:     3000,
				CompletionTokens: 1200,
				TotalTokens:      4200,
				WallTimeSeconds:  30.0,
				ToolCalls:        5,
			},
		},
		StartedAt: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}

	if err := SaveState(path, original); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if len(loaded.PhaseTokens) != 3 {
		t.Fatalf("PhaseTokens count = %d, want 3", len(loaded.PhaseTokens))
	}

	for i, want := range original.PhaseTokens {
		got := loaded.PhaseTokens[i]
		if got.Phase != want.Phase {
			t.Errorf("[%d] Phase = %q, want %q", i, got.Phase, want.Phase)
		}
		if got.Model != want.Model {
			t.Errorf("[%d] Model = %q, want %q", i, got.Model, want.Model)
		}
		if got.PromptTokens != want.PromptTokens {
			t.Errorf("[%d] PromptTokens = %d, want %d", i, got.PromptTokens, want.PromptTokens)
		}
		if got.CompletionTokens != want.CompletionTokens {
			t.Errorf("[%d] CompletionTokens = %d, want %d", i, got.CompletionTokens, want.CompletionTokens)
		}
		if got.TotalTokens != want.TotalTokens {
			t.Errorf("[%d] TotalTokens = %d, want %d", i, got.TotalTokens, want.TotalTokens)
		}
		if got.EstimatedCostUSD != want.EstimatedCostUSD {
			t.Errorf("[%d] EstimatedCostUSD = %f, want %f", i, got.EstimatedCostUSD, want.EstimatedCostUSD)
		}
		if got.WallTimeSeconds != want.WallTimeSeconds {
			t.Errorf("[%d] WallTimeSeconds = %f, want %f", i, got.WallTimeSeconds, want.WallTimeSeconds)
		}
		if got.ToolCalls != want.ToolCalls {
			t.Errorf("[%d] ToolCalls = %d, want %d", i, got.ToolCalls, want.ToolCalls)
		}
	}
}

func TestPhaseTokensOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &State{
		RepoOwner:   "ruromero",
		RepoName:    "la-fabriquilla",
		IssueNumber: 50,
		Phase:       "plan",
		StartedAt:   time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}

	if err := SaveState(path, original); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// phase_tokens should not appear in JSON when empty (omitempty)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if _, exists := raw["phase_tokens"]; exists {
		t.Error("phase_tokens should be omitted when empty")
	}
}

func TestRecordTokenUsage(t *testing.T) {
	s := &State{}

	// First record: Gemini model with known cost
	s.RecordTokenUsage("researcher", "gemini-2.5-flash", 1000, 500, 0, 2.5)

	if len(s.PhaseTokens) != 1 {
		t.Fatalf("PhaseTokens count = %d, want 1", len(s.PhaseTokens))
	}
	pt := s.PhaseTokens[0]
	if pt.Phase != "researcher" {
		t.Errorf("Phase = %q, want %q", pt.Phase, "researcher")
	}
	if pt.TotalTokens != 1500 {
		t.Errorf("TotalTokens = %d, want 1500", pt.TotalTokens)
	}
	expectedCost := EstimateCost("gemini-2.5-flash", 1000, 500)
	if pt.EstimatedCostUSD != expectedCost {
		t.Errorf("EstimatedCostUSD = %f, want %f", pt.EstimatedCostUSD, expectedCost)
	}
	if pt.ToolCalls != 0 {
		t.Errorf("ToolCalls = %d, want 0", pt.ToolCalls)
	}
	if s.TotalPromptTokens != 1000 {
		t.Errorf("TotalPromptTokens = %d, want 1000", s.TotalPromptTokens)
	}
	if s.TotalCompTokens != 500 {
		t.Errorf("TotalCompTokens = %d, want 500", s.TotalCompTokens)
	}
	if s.TotalCostUSD != expectedCost {
		t.Errorf("TotalCostUSD = %f, want %f", s.TotalCostUSD, expectedCost)
	}

	// Second record: Ollama model (zero cost) with tool calls
	s.RecordTokenUsage("gatherer", "qwen3:14b", 2000, 800, 12, 15.3)

	if len(s.PhaseTokens) != 2 {
		t.Fatalf("PhaseTokens count = %d, want 2", len(s.PhaseTokens))
	}
	pt2 := s.PhaseTokens[1]
	if pt2.TotalTokens != 2800 {
		t.Errorf("TotalTokens = %d, want 2800", pt2.TotalTokens)
	}
	if pt2.EstimatedCostUSD != 0 {
		t.Errorf("EstimatedCostUSD = %f, want 0 (local model)", pt2.EstimatedCostUSD)
	}
	if pt2.ToolCalls != 12 {
		t.Errorf("ToolCalls = %d, want 12", pt2.ToolCalls)
	}

	// Cumulative totals
	if s.TotalPromptTokens != 3000 {
		t.Errorf("TotalPromptTokens = %d, want 3000", s.TotalPromptTokens)
	}
	if s.TotalCompTokens != 1300 {
		t.Errorf("TotalCompTokens = %d, want 1300", s.TotalCompTokens)
	}
	if s.TotalCostUSD != expectedCost {
		t.Errorf("TotalCostUSD = %f, want %f (only Gemini has cost)", s.TotalCostUSD, expectedCost)
	}
}

func TestLoadStateNotFound(t *testing.T) {
	_, err := LoadState("/nonexistent/path/state.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReviewStateLegacyFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	legacyJSON := `{
		"repo_owner": "ruromero",
		"repo_name": "la-fabriquilla",
		"issue_number": 99,
		"phase": "review-done",
		"issue_title": "test",
		"issue_body": "test",
		"summaries": "",
		"conventions": "",
		"review": {
			"correctness": "[CRITICAL] Nil pointer — unchecked before use",
			"security": "[PASS] No security issues found.",
			"intent": "[ALIGNED]"
		},
		"started_at": "2026-05-30T12:00:00Z",
		"updated_at": "2026-05-30T12:00:00Z"
	}`

	if err := os.WriteFile(path, []byte(legacyJSON), 0600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.Review == nil {
		t.Fatal("Review is nil after loading legacy format")
	}
	if len(loaded.Review.Findings) != 1 {
		t.Fatalf("Findings count = %d, want 1 (one CRITICAL from correctness)", len(loaded.Review.Findings))
	}
	f := loaded.Review.Findings[0]
	if f.Severity != review.SeverityCritical {
		t.Errorf("severity = %q, want %q", f.Severity, review.SeverityCritical)
	}
	if f.Category != review.CategoryCorrectness {
		t.Errorf("category = %q, want %q", f.Category, review.CategoryCorrectness)
	}
}
