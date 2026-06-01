# Review-Iterate Loop — Design Spec

**Issue:** #65 — Wire review-iterate loop into dispatcher  
**Date:** 2026-06-01

## Overview

Move the review-iterate loop from an internal coder concern to a
dispatcher-level orchestration step. After the committer creates a PR,
the dispatcher runs a reviewer (different model family) and, if critical
or medium findings exist, loops through iterator + reviewer until the
review is clean or max iterations are reached. The coder's internal
review loop is preserved as a fast same-model self-correction pass.

## New Binaries

### cmd/reviewer/main.go

One-shot binary, same pattern as other phase binaries:

1. `helpers.MustLoadConfigAndState()`.
2. Create inference client from `cfg.Inference` — the sandbox policy
   enforces which endpoint is reachable (DeepSeek for review, not
   Ollama). No separate config field needed.
3. Call `agents.Review()` with `state.Code`, `state.Design`,
   `state.PlanContent`, `state.Conventions`.
4. Save `ReviewResult` to `state.Review`.
5. Record token usage, set `state.Phase = "review-done"`, save state.

### cmd/iterator/main.go

One-shot binary:

1. `helpers.MustLoadConfigAndState()`.
2. Create inference client (Ollama, same as coder).
3. Format review feedback via `pipeline.FormatReviewFeedback()` from
   `state.Review`.
4. Call `agents.Iterate()` with current code and formatted feedback.
5. Parse output to extract updated files.
6. Push fixup commit: create GitHub client, call
   `gh.CreateCommit(ctx, state.PRBranch, commitMsg, files)` on the
   existing PR branch.
7. Update `state.Code`, `state.Files`, increment `state.Iteration`,
   set `state.Phase = "iterate-done"`, save state.

## Dispatcher Changes

### reviewIterateLoop helper

Called from `processIssue()` after the committer phase succeeds and
`state.PRNumber > 0`.

```
func reviewIterateLoop(ctx, cfg, store, key, statePath, sandboxImage, issueNumber) error
    for i := 0; i < cfg.MaxIterations; i++
        runPhase("reviewer", ...)
        reload state
        CheckCostBudget(state, cfg.MaxCostBudget)

        if !ReviewNeedsIteration(state.Review)
            log "review clean after {i} iterations"
            return nil

        log "review found issues, iteration {i+1}/{max}"
        runPhase("iterator", ...)
        reload state
        CheckCostBudget(state, cfg.MaxCostBudget)

    log.Warn "max review iterations reached"
    return nil
```

**Key decisions:**

- Max iterations exhausted is **not an error** — the PR was already
  created; it stays open with unresolved findings for human review.
- Cost budget checked after each phase.
- No scope/content re-validation — the iterator only modifies existing
  files to fix review findings, not add new ones. The scope check ran
  before the committer.

### Phase registration

- `sandboxMVPPhases`: add `"reviewer": true` and `"iterator": true`.
- `noRetryPhases`: add `"iterator": true` (pushes commits,
  non-idempotent).

## Sandbox Policies

### deploy/sandbox-policies/reviewer.yaml (exists)

Already configured for DeepSeek only:

```yaml
network:
  default: deny
  allow:
    - endpoint: "api.deepseek.com:443"
      methods: [POST]
      paths: ["/v1/chat/completions"]
```

### deploy/sandbox-policies/iterator.yaml (update)

Add GitHub API access for fixup commits:

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

## Makefile

Add `reviewer` and `iterator` to `BINARIES`:

```makefile
BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator eval
```

## State

No new fields. Existing fields used:

- `Review *ReviewState` — correctness, security, intent
- `Iteration int` — loop counter
- `PRNumber int`, `PRBranch string` — iterator uses for fixup commits
- `Files []FileState` — updated by iterator
- `Phase string` — new values: `"review-done"`, `"iterate-done"`

## Two-Layer Review Architecture

The coder's internal review loop (same model, fast self-correction) is
**preserved**. The dispatcher-level loop adds a second layer using a
different model family (DeepSeek vs Ollama), enforced by sandbox
policies. This satisfies the security invariant that review must use a
different model family than code generation.

## Tests

Unit tests for `reviewIterateLoop` covering three termination
conditions:

1. **Clean review** — first review returns all PASS, loop exits
   immediately.
2. **Max iterations** — review always returns CRITICAL, loop runs
   `MaxIterations` times then returns nil.
3. **Converges mid-loop** — review returns CRITICAL first, PASS after
   one iteration.

Tests use an injected `runPhase` function to avoid real binary
execution.
