package testutil

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/review"
)

// CannedPhaseRunner simulates phase execution by applying deterministic
// state transitions. Used in full-mock smoke tests (no GPU, no network).
type CannedPhaseRunner struct{}

func NewCannedPhaseRunner() *CannedPhaseRunner {
	return &CannedPhaseRunner{}
}

// Run loads state from statePath, applies the phase transformation, and saves.
func (r *CannedPhaseRunner) Run(_ context.Context, phase, statePath string) error {
	state, err := pipeline.LoadState(statePath)
	if err != nil {
		return fmt.Errorf("load state for %s: %w", phase, err)
	}

	switch phase {
	case "gatherer":
		state.Phase = "gathered"
		state.GatheredContext = "Codebase: single Go package with main.go (50 lines). Uses standard library only."
		state.RecordTokenUsage("gatherer", "canned", 100, 50, 0, 1.0)

	case "researcher":
		state.Phase = "researched"
		state.ResearchContext = "No external libraries needed. Standard Go patterns apply."
		state.RecordTokenUsage("researcher", "canned", 200, 100, 0, 2.0)

	case "planner":
		state.Phase = "planned"
		state.PlanOutcome = "plan"
		state.PlanContent = "1. Add hello() function to main.go\n2. Add unit test in main_test.go"
		state.RecordTokenUsage("planner", "canned", 300, 150, 0, 3.0)

	case "designer":
		state.Phase = "designed"
		state.Design = "func hello() string { return \"hello\" }"
		state.RecordTokenUsage("designer", "canned", 150, 75, 0, 1.5)

	case "coder":
		state.Phase = "coded"
		state.Files = []pipeline.FileState{
			{Path: "hello.go", Content: "package main\n\nfunc hello() string {\n\treturn \"hello\"\n}\n"},
			{Path: "hello_test.go", Content: "package main\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {\n\tif got := hello(); got != \"hello\" {\n\t\tt.Errorf(\"hello() = %q, want %q\", got, \"hello\")\n\t}\n}\n"},
		}
		state.RecordTokenUsage("coder", "canned", 500, 250, 3, 5.0)

	case "committer":
		state.Phase = "committed"
		state.PRNumber = 42
		state.PRBranch = "factory/issue-1-add-hello"
		state.RecordTokenUsage("committer", "canned", 50, 25, 0, 0.5)

	case "reviewer":
		state.Phase = "reviewed"
		state.Review = &pipeline.ReviewState{
			Findings: []review.ReviewFinding{},
		}
		state.RecordTokenUsage("reviewer", "canned", 400, 200, 0, 4.0)

	case "iterator":
		state.Phase = "iterated"
		state.Iteration++
		state.RecordTokenUsage("iterator", "canned", 300, 150, 2, 3.0)

	default:
		return fmt.Errorf("unknown phase: %s", phase)
	}

	return pipeline.SaveState(statePath, state)
}
