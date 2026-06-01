# Review-Iterate Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the review-iterate loop into the dispatcher as separate reviewer/iterator binaries that run after the committer creates a PR, using a different model family for review.

**Architecture:** Two new one-shot binaries (`cmd/reviewer/`, `cmd/iterator/`) follow the existing phase binary pattern. The dispatcher gains a `reviewIterateLoop` helper called after the committer phase. The reviewer calls `agents.Review()` (sandboxed to DeepSeek). The iterator calls `agents.IterateWithUsage()` (sandboxed to Ollama) and pushes fixup commits to the existing PR branch. The coder's internal review loop is preserved as a first-pass self-correction.

**Tech Stack:** Go 1.26+, stdlib only, OpenAI-compatible inference API

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `cmd/reviewer/main.go` | One-shot reviewer binary — loads state, runs review, saves results |
| Create | `cmd/iterator/main.go` | One-shot iterator binary — loads state, runs iteration, pushes fixup commit, saves results |
| Modify | `cmd/dispatcher/main.go` | Add `reviewIterateLoop` helper, call it after committer, register sandbox/no-retry phases |
| Create | `cmd/dispatcher/review_loop_test.go` | Tests for loop termination conditions |
| Modify | `deploy/sandbox-policies/iterator.yaml` | Add GitHub API access for fixup commits |
| Modify | `Makefile` | Add `reviewer` and `iterator` to `BINARIES` |

---

### Task 1: Create `cmd/reviewer/main.go`

**Files:**
- Create: `cmd/reviewer/main.go`

- [ ] **Step 1: Create the reviewer binary**

Create `cmd/reviewer/main.go` following the `cmd/designer/main.go` pattern (simplest existing binary). The reviewer loads state, creates an inference client, calls `agents.Review()`, saves the review result to state.

The reviewer needs gather tools (for Serena-based file reading during review) just like the coder's review call does. It also needs a GitHub client to build the `RepoContext` for `BuildGatherTools`.

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/harness"
	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/mcp"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/traces"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	cl := inference.NewClient(cfg.Inference.BaseURL, inference.WithAPIKey(cfg.Inference.APIKey))
	ctx := context.Background()

	var sess *harness.SerenaSession
	if state.CloneDir != "" {
		var err error
		sess, err = harness.StartSerenaFromClone(ctx, state.CloneDir, cfg.Serena)
		if err != nil {
			slog.Warn("failed to start Serena", "error", err)
		}
	}
	if sess != nil {
		defer sess.Cleanup()
	}

	var serenaClient *mcp.Client
	if sess != nil {
		serenaClient = sess.Client
	}

	gh := helpers.MustGitHubClientForApp(cfg, "worker", state)
	rc := harness.LoadRepoContext(ctx, gh)
	tools, handler := harness.BuildGatherTools(rc, gh, serenaClient)

	start := time.Now()
	review, err := agents.Review(ctx, cl, state.Code, state.Design, state.PlanContent, state.Conventions, tools, handler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("review phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("reviewer", review.Model, review.PromptTokens, review.CompTokens, review.ToolCalls, elapsed.Seconds())
	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "reviewer",
		Model:           review.Model,
		PromptTokens:    review.PromptTokens,
		CompTokens:      review.CompTokens,
		ToolCalls:       review.ToolCalls,
		Duration:        elapsed.String(),
		StartedAt:       start,
		CumPromptTokens: state.TotalPromptTokens,
		CumCompTokens:   state.TotalCompTokens,
		CumCostUSD:      state.TotalCostUSD,
	})

	state.Review = &pipeline.ReviewState{
		Correctness: review.Correctness,
		Security:    review.Security,
		Intent:      review.Intent,
	}
	state.Phase = "review-done"
	helpers.MustSaveState(state)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `CGO_ENABLED=0 go build ./cmd/reviewer/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add cmd/reviewer/main.go
git commit -m "feat: add reviewer one-shot binary (#65)"
```

---

### Task 2: Create `cmd/iterator/main.go`

**Files:**
- Create: `cmd/iterator/main.go`

- [ ] **Step 1: Create the iterator binary**

Create `cmd/iterator/main.go`. This binary loads state, runs `agents.IterateWithUsage()`, parses the output into files, pushes a fixup commit to the existing PR branch, and saves state.

The iterator needs Serena tools (for code editing during iteration) and a GitHub client (for pushing the fixup commit). It uses the `"worker"` app role like the coder does, not `"committer"`, since it's making code changes, and the committer app identity is reserved for creating PRs.

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/harness"
	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/mcp"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/traces"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	cl := inference.NewClient(cfg.Inference.BaseURL, inference.WithAPIKey(cfg.Inference.APIKey))
	ctx := context.Background()

	var sess *harness.SerenaSession
	if state.CloneDir != "" {
		var err error
		sess, err = harness.StartSerenaFromClone(ctx, state.CloneDir, cfg.Serena)
		if err != nil {
			slog.Warn("failed to start Serena", "error", err)
		}
	}
	if sess != nil {
		defer sess.Cleanup()
	}

	var serenaClient *mcp.Client
	if sess != nil {
		serenaClient = sess.Client
	}
	tools, handler := harness.BuildCoderTools(serenaClient)

	if state.Review == nil {
		slog.Error("no review findings in state")
		os.Exit(1)
	}
	feedback := pipeline.FormatReviewFeedback(state.Review.Correctness, state.Review.Security, state.Review.Intent)

	start := time.Now()
	iterResult, err := agents.IterateWithUsage(ctx, cl, state.Code, feedback, tools, handler)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("iterate phase failed", "error", err)
		os.Exit(1)
	}

	state.RecordTokenUsage("iterator", iterResult.Model, iterResult.PromptTokens, iterResult.CompTokens, iterResult.ToolCalls, elapsed.Seconds())
	traces.Log(traces.Trace{
		IssueNumber:     state.IssueNumber,
		Phase:           "iterator",
		Model:           iterResult.Model,
		PromptTokens:    iterResult.PromptTokens,
		CompTokens:      iterResult.CompTokens,
		ToolCalls:       iterResult.ToolCalls,
		Duration:        elapsed.String(),
		StartedAt:       start,
		CumPromptTokens: state.TotalPromptTokens,
		CumCompTokens:   state.TotalCompTokens,
		CumCostUSD:      state.TotalCostUSD,
	})

	parsed, err := pipeline.ParseStructuredCodeOutput(iterResult.Content)
	if err != nil {
		slog.Info("structured parse failed, falling back to regex", "error", err)
		parsed = pipeline.ParseCodeOutput(iterResult.Content)
	}

	if state.PRBranch != "" {
		gh := helpers.MustGitHubClientForApp(cfg, "committer", state)
		files := make([]github.FileChange, len(parsed))
		for i, f := range parsed {
			files[i] = github.FileChange{Path: f.Path, Content: f.Content}
		}
		commitMsg := fmt.Sprintf("fix: address review findings (#%d, iteration %d)", state.IssueNumber, state.Iteration+1)
		if _, err := gh.CreateCommit(ctx, state.PRBranch, commitMsg, files); err != nil {
			slog.Error("failed to push fixup commit", "error", err)
			os.Exit(1)
		}
		slog.Info("pushed fixup commit", "branch", state.PRBranch, "iteration", state.Iteration+1)
	}

	state.Code = iterResult.Content
	state.Files = parsed
	state.Iteration++
	state.Phase = "iterate-done"
	helpers.MustSaveState(state)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `CGO_ENABLED=0 go build ./cmd/iterator/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add cmd/iterator/main.go
git commit -m "feat: add iterator one-shot binary with fixup commits (#65)"
```

---

### Task 3: Write `reviewIterateLoop` tests

**Files:**
- Create: `cmd/dispatcher/review_loop_test.go`

- [ ] **Step 1: Write the failing tests**

The tests exercise the `reviewIterateLoop` function through three termination conditions. The loop takes a `phaseRunner` function type so we can inject test behavior without running real binaries. We control the review results by writing state files that the loop reads back.

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/pipeline"
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
				Correctness: "[PASS] No issues found.",
				Security:    "[PASS] No security issues found.",
				Intent:      "[ALIGNED]",
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
				Correctness: "[CRITICAL] Bug — something is wrong",
				Security:    "[PASS] No security issues found.",
				Intent:      "[ALIGNED]",
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
					Correctness: "[CRITICAL] Bug — something is wrong",
					Security:    "[PASS] No security issues found.",
					Intent:      "[ALIGNED]",
				}
			} else {
				s.Review = &pipeline.ReviewState{
					Correctness: "[PASS] No issues found.",
					Security:    "[PASS] No security issues found.",
					Intent:      "[ALIGNED]",
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
```

**Note on `writeTestState` / `key` / `store`:** The `pipeline.NewFileStateStore` uses `key` as a subdirectory or filename stem inside `dir`. For tests, we use `"state"` as the key and write the state file to the path that the store would expect. Check the actual `StateKey`/`StatePath` behavior — the `store.Load(ctx, key)` call in the loop must find the file that `runner` writes. Since the runner receives `statePath` and writes there, and the loop uses `store.Load(ctx, key)` which resolves to the same path, this should work as long as `key` matches how `NewFileStateStore` builds the path. Verify by looking at `pipeline.NewFileStateStore` and `StatePath` — adjust the key if needed.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestReviewIterateLoop ./cmd/dispatcher/ -v`
Expected: compilation error — `reviewIterateLoop` not defined yet

- [ ] **Step 3: Commit**

```bash
git add cmd/dispatcher/review_loop_test.go
git commit -m "test: add review-iterate loop termination tests (#65)"
```

---

### Task 4: Implement `reviewIterateLoop` in the dispatcher

**Files:**
- Modify: `cmd/dispatcher/main.go`

- [ ] **Step 1: Add the `phaseRunner` type and `reviewIterateLoop` function**

Add the following after the `createSubIssues` function at the bottom of `cmd/dispatcher/main.go`:

```go
type phaseRunner func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error

func reviewIterateLoop(ctx context.Context, cfg *config.Config, store *pipeline.FileStateStore, key, statePath, sandboxImage string, issueNumber int, runner phaseRunner) error {
	for i := 0; i < cfg.MaxIterations; i++ {
		slog.Info("starting review iteration", "iteration", i+1, "max", cfg.MaxIterations)

		if err := runner(ctx, cfg, "reviewer", statePath, issueNumber, sandboxImage); err != nil {
			return fmt.Errorf("reviewer (iteration %d): %w", i+1, err)
		}

		state, err := store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after review (iteration %d): %w", i+1, err)
		}
		if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after review (iteration %d): %w", i+1, err)
		}

		if state.Review == nil || !pipeline.ReviewNeedsIteration(state.Review.Correctness, state.Review.Security, state.Review.Intent) {
			slog.Info("review clean", "iterations", i+1)
			return nil
		}

		slog.Info("review found issues, running iterator", "iteration", i+1)
		if err := runner(ctx, cfg, "iterator", statePath, issueNumber, sandboxImage); err != nil {
			return fmt.Errorf("iterator (iteration %d): %w", i+1, err)
		}

		state, err = store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after iterate (iteration %d): %w", i+1, err)
		}
		if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after iterate (iteration %d): %w", i+1, err)
		}
	}

	slog.Warn("max review iterations reached", "max", cfg.MaxIterations)
	return nil
}
```

- [ ] **Step 2: Wire the loop into `processIssue`**

In `processIssue()`, after the committer phase block (after line 346 where `state.PRNumber` is logged), add the review-iterate loop call. Replace the existing block:

```go
		if state.PRNumber > 0 {
			log.Info("PR created", "pr", state.PRNumber)
		}
		return nil
```

With:

```go
		if state.PRNumber > 0 {
			log.Info("PR created, starting review loop", "pr", state.PRNumber)
			runner := func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error {
				return runPhase(ctx, cfg, binary, statePath, issueNumber, sandboxImage)
			}
			if err := reviewIterateLoop(ctx, &cfg, store, key, statePath, sandboxImage, issue.Number, runner); err != nil {
				log.Warn("review-iterate loop failed", "error", err)
			}
		}
		return nil
```

Note: review-iterate loop errors are logged as warnings, not returned as errors. The PR was already created — the loop is a best-effort improvement, not a gate.

- [ ] **Step 3: Register sandbox and no-retry phases**

Update the `sandboxMVPPhases` map (around line 361):

```go
var sandboxMVPPhases = map[string]bool{
	"coder":    true,
	"reviewer": true,
	"iterator": true,
}
```

Update the `noRetryPhases` map (around line 355):

```go
var noRetryPhases = map[string]bool{
	"committer": true,
	"iterator":  true,
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -run TestReviewIterateLoop ./cmd/dispatcher/ -v`
Expected: all 3 tests PASS

- [ ] **Step 5: Run full test suite**

Run: `go test -race ./...`
Expected: all tests PASS

- [ ] **Step 6: Verify build**

Run: `CGO_ENABLED=0 go build ./...`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add cmd/dispatcher/main.go
git commit -m "feat: wire review-iterate loop into dispatcher (#65)"
```

---

### Task 5: Update sandbox policy and Makefile

**Files:**
- Modify: `deploy/sandbox-policies/iterator.yaml`
- Modify: `Makefile`

- [ ] **Step 1: Update iterator sandbox policy**

Add GitHub API access for fixup commits. Replace the contents of `deploy/sandbox-policies/iterator.yaml` with:

```yaml
network:
  default: deny
  allow:
    - endpoint: "ollama.ai.svc.cluster.local:11434"
      methods: [POST]
      paths: ["/v1/chat/completions"]
    - endpoint: "api.github.com:443"
      methods: [POST, PUT, GET]
      paths: ["/repos/"]
```

- [ ] **Step 2: Update Makefile**

Change line 1 of `Makefile` from:

```makefile
BINARIES := dispatcher gatherer researcher planner designer coder committer eval
```

To:

```makefile
BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator eval
```

- [ ] **Step 3: Verify full build with new binaries**

Run: `make build`
Expected: all binaries build successfully, including `bin/reviewer` and `bin/iterator`

- [ ] **Step 4: Run all checks**

Run: `make check`
Expected: fmt, vet, and test all pass

- [ ] **Step 5: Commit**

```bash
git add deploy/sandbox-policies/iterator.yaml Makefile
git commit -m "feat: update sandbox policy and Makefile for reviewer/iterator (#65)"
```

---

### Task 6: Final verification

- [ ] **Step 1: Run full check suite**

Run: `make check`
Expected: fmt, vet, and all tests pass

- [ ] **Step 2: Verify all new binaries exist**

Run: `ls -la bin/reviewer bin/iterator`
Expected: both binaries exist after `make build`

- [ ] **Step 3: Run `go vet` and `gofmt`**

Run: `gofmt -l . && go vet ./...`
Expected: no output (no formatting issues, no vet warnings)
