# Architecture — Multi-App Sandboxed Factory

## Overview

The la-fabriquilla is a set of purpose-built Go binaries that poll GitHub
repos for issues tagged `fabriquilla:ready`, run them through a phased LLM
pipeline, and open PRs with the results. The system separates concerns into
scoped binaries with optional OpenShell sandbox isolation, pluggable external
review, and deterministic guardrails.

This document is the authoritative reference for the architecture.

---

## 1. Binary Layout

Single Go source repo (`go.mod`), multiple `cmd/` entries. Shared library
packages are unchanged — each binary imports only what it needs. The Go
compiler dead-code-eliminates unused packages per binary.

```
cmd/
  dispatcher/main.go     long-running: poll loop, triage, phase orchestration
  gatherer/main.go       one-shot: context gathering via Ollama + Serena
  researcher/main.go     one-shot: external research via Gemini
  planner/main.go        one-shot: planning via OpenAI-compatible API
  designer/main.go       one-shot: technical design via Ollama
  coder/main.go          one-shot: code generation via Ollama + Serena
  committer/main.go      one-shot: branch, commit, PR creation, merge
  reviewer/main.go       one-shot: 3-pass review + arbiter classification
  iterator/main.go       one-shot: apply review feedback via Ollama + Serena
  eval/main.go           golden-set evaluation harness
  eval-runner/main.go    model comparison eval runner (name@endpoint syntax)
  internal/helpers.go    shared helpers (config/state loading, GitHub client)

  ### Planned binaries (not yet implemented)
  feedback/main.go       one-shot: external review loop + feedback capture
  dashboard/main.go      web UI: config, monitoring, reports, control
    frontend/            React app (embedded via embed.FS)
```

### Shared packages

```
config/      Configuration loading and validation
pipeline/    State management, parsing, validation, guardrails
agents/      Pure functions: one per pipeline phase (7 agents)
harness/     Context assembly, tool handling, LSP, Serena integration
openshell/   OpenShell sandbox lifecycle management
sandbox/     Input sanitization (Unicode, secrets redaction, URL validation)
eval/        Golden-set test case runner and reporting
github/      GitHub REST API client, App auth, readiness checks
inference/   OpenAI-compatible inference client + tool-calling loop
gemini/      Gemini API client (research phase)
review/      Unified review finding types, ExternalReviewAdapter interface, QodoAdapter
mcp/         MCP client (JSON-RPC over stdio)
traces/      Structured JSON trace logging (minimal)
```

### Build

```makefile
BINARIES := dispatcher gatherer researcher planner designer coder committer reviewer iterator eval eval-runner

build: $(BINARIES)

$(BINARIES):
	CGO_ENABLED=0 go build -o bin/$@ ./cmd/$@/
```

---

## 2. GitHub App Identity and Permissions

Three GitHub Apps, aligned to trust boundaries.

| GitHub App | Binaries | Permissions | Rationale |
|---|---|---|---|
| **fabriquilla-dispatcher** | dispatcher | issues:write, contents:read | Read/write issues and labels, read repo files for readiness and context. Cannot create branches or PRs. |
| **fabriquilla-worker** | gatherer, coder, iterator | contents:read | Read repo contents and clone for Serena. Cannot write issues, create branches, or open PRs. |
| **fabriquilla-committer** | committer, feedback | contents:write, pull_requests:write, issues:write | Create branches, commits, PRs, and relabel issues. The only identity that can push code. |

**researcher**, **planner**, **designer**, **reviewer** need zero GitHub
credentials. All inputs come from pipeline state. All outputs go to pipeline
state.

### Configuration

```json
{
  "apps": {
    "dispatcher": {
      "app_id": 111,
      "installation_id": 222,
      "private_key_path": "/keys/dispatcher.pem"
    },
    "worker": {
      "app_id": 333,
      "installation_id": 444,
      "private_key_path": "/keys/worker.pem"
    },
    "committer": {
      "app_id": 555,
      "installation_id": 666,
      "private_key_path": "/keys/committer.pem"
    }
  }
}
```

### Planned: Scoped installation tokens

Extend `github/app_auth.go` with per-binary permission scoping:

```go
func (a *AppAuth) TokenWithPermissions(ctx context.Context, perms map[string]string) (string, error)
```

Each binary would request tokens with only its required permissions. If a
scoped token leaks from a sandbox, its blast radius is limited.

---

## 3. Pipeline State Management

### State struct

All inter-phase data in a single JSON-serializable struct (`pipeline/state.go`):

```go
type State struct {
    RepoOwner   string `json:"repo_owner"`
    RepoName    string `json:"repo_name"`
    IssueNumber int    `json:"issue_number"`

    Phase     string `json:"phase"`
    Iteration int    `json:"iteration"`

    IssueTitle     string `json:"issue_title"`
    IssueBody      string `json:"issue_body"`
    CommentHistory string `json:"comment_history,omitempty"`
    Summaries      string `json:"summaries"`
    Conventions    string `json:"conventions"`

    GatheredContext string        `json:"gathered_context,omitempty"`
    ResearchContext string        `json:"research_context,omitempty"`
    PlanOutcome     string        `json:"plan_outcome,omitempty"`
    PlanContent     string        `json:"plan_content,omitempty"`
    Design          string        `json:"design,omitempty"`
    Code            string        `json:"code,omitempty"`
    Review          *ReviewState  `json:"review,omitempty"`
    ArbiterResult   *ArbiterState `json:"arbiter_result,omitempty"`
    Files           []FileState   `json:"files,omitempty"`

    PRNumber int    `json:"pr_number,omitempty"`
    PRBranch string `json:"pr_branch,omitempty"`

    StartedAt time.Time `json:"started_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

### Storage

File-backed: `/data/pipeline/{owner}/{repo}/{issue_number}.json`

```go
type StateStore interface {
    Save(ctx context.Context, key string, state *State) error
    Load(ctx context.Context, key string) (*State, error)
}
```

Each phase binary reads state, does its work, updates state, and saves.

### File validation

`pipeline/validate.go` — before committing, the committer validates all file paths:

- Rejects path traversal (`../`, absolute paths)
- Rejects blocked patterns (see Guardrails section)
- Rejects empty paths or content
- Detects embedded secrets in file contents

---

## 4. OpenShell Sandbox Integration

### What runs inside vs outside sandboxes

| Component | Sandboxed | Network policy | Why |
|---|---|---|---|
| dispatcher | No | N/A | Trusted deterministic code, needs full GitHub API access |
| gatherer | Yes | Ollama only | Runs LLM with tool calls against cloned repo |
| researcher | Yes | generativelanguage.googleapis.com | Calls external Gemini API |
| planner | Yes | configured planner endpoint | Calls external API |
| designer | Yes | Ollama only | Calls local LLM |
| coder | Yes | Ollama only | Runs LLM with read+write Serena tools |
| reviewer | Yes | DeepSeek API endpoint | High-judgment arbitration via external API |
| iterator | Yes | Ollama only | Applies fixes with LLM + Serena tools |
| committer | No | N/A | Trusted deterministic code, needs GitHub write access |
| feedback | No | N/A | Trusted deterministic code, needs GitHub API access |

The dispatcher and committer are deterministic Go code with no LLM
interaction. Sandboxing them adds latency for zero security benefit.

### Go client for OpenShell

The `openshell/` package wraps the OpenShell CLI for sandbox lifecycle:

- `RunInSandbox()` — full lifecycle (create, upload state, exec phase binary, download state, destroy)
- `SandboxName()` — deterministic naming (`factory-{phase}-{issue}`)
- `SandboxConfig` struct with validation

### Network policy files

```
deploy/sandbox-policies/
  gatherer.yaml
  researcher.yaml
  planner.yaml
  designer.yaml
  coder.yaml
  reviewer.yaml
  iterator.yaml
```

The coder sandbox has NO access to `api.github.com`. Even if the LLM is
manipulated via prompt injection, it physically cannot push code.

### GPU considerations

Ollama runs as a system service on the host, not inside sandboxes. Sandboxes
call Ollama over HTTP — no GPU passthrough needed.

---

## 5. Sandbox Images

Language-specific images extend a common base. The dispatcher selects the
right image based on the target repo's language (configured per-repo).

```
deploy/sandbox-images/
  base/Dockerfile          git, gh CLI, python3, Serena, all factory phase binaries
  go/Dockerfile            extends base: Go toolchain + gopls
  rust/Dockerfile          extends base: Rust toolchain + rust-analyzer
  typescript/Dockerfile    extends base: Node + typescript-language-server
```

### Per-repo configuration

```json
{
  "repos": [
    {
      "owner": "ruromero",
      "repo": "la-fabriquilla",
      "language": "go",
      "sandbox_image": "factory-go:latest"
    }
  ]
}
```

---

## 6. Review Architecture

### Current state

Review is a multi-source pipeline with arbiter classification:

```
Factory reviewer (reviewer binary, 3-pass via Ollama)
  Correctness, security, intent alignment passes
  Produces structured ReviewFinding[]

External reviewer (pluggable — Qodo shipped)
  Parsed through ExternalReviewAdapter interface
  Produces structured ReviewFinding[]

        ↓ all findings ↓

Arbiter (part of reviewer binary, configurable endpoint e.g. DeepSeek)
  Classifies each → fix_here | subtask | root_cause | dismissed
  Produces ArbiterResult
```

### Pluggable external reviewer

The `ExternalReviewAdapter` interface and `QodoAdapter` are implemented
in the `review/` package:

```go
type ExternalReviewAdapter interface {
    ParseFindings(ctx context.Context, client PRCommentClient, prNumber int) ([]ReviewFinding, error)
    TriggerReview(ctx context.Context, gh *github.Client, prNumber int) error
    ReviewReady(ctx context.Context, comments []github.Comment) bool
}
```

Shipped adapters: `QodoAdapter` (parse `/agentic_review` output).

Planned adapter: `HumanAdapter` (parse GitHub PR review comments).

### Arbiter behavior

The arbiter classifies each finding:
- **fix_here**: simple fix, iterator can handle in this PR
- **subtask**: needs planning but belongs in this PR
- **root_cause**: systemic issue requiring its own issue lifecycle
- **dismissed**: invalid given project context (with stated reason)

If a finding was dismissed in iteration N and reappears in N+1:
auto-dismiss to prevent deadlock. This deadlock prevention is shipped.

---

## 7. Pipeline Flow

```
1. Human creates GitHub issue, adds label "fabriquilla:ready"

2. Dispatcher polls GitHub, finds issue
   ├── Checks repo readiness (required files)
   ├── Swaps label: fabriquilla:ready → fabriquilla:in-progress
   ├── Loads repo context (README, ARCHITECTURE, CONVENTIONS)
   ├── Sanitizes issue title/body
   ├── Loads human comment history
   ├── Initializes pipeline State, saves to disk
   │
   ├── Phase: Gather (sandbox, parallel with research)
   │     Network: Ollama only
   │     Input: issue title/body + repo summaries
   │     Output: gathered context → State
   │
   ├── Phase: Research (sandbox, parallel with gather)
   │     Network: Gemini API only
   │     Input: issue title/body + repo summaries
   │     Output: research context → State
   │
   ├── Phase: Plan (sandbox)
   │     Network: planner API only
   │     Input: gathered + research context + conventions + comments
   │     Output: plan outcome + content → State
   │     │
   │     ├── needs_info → comment on issue, label fabriquilla:needs-info, stop
   │     ├── decompose → create sub-issues, label fabriquilla:tracking, stop
   │     └── plan → continue
   │
   ├── Phase: Design (sandbox)
   │     Network: Ollama only
   │     Input: plan + research context + conventions
   │     Output: technical design → State
   │
   ├── Phase: Code (sandbox)
   │     Network: Ollama only
   │     Input: design + research context + conventions
   │     Tools: Serena read+write
   │     Output: code + parsed files → State
   │
   ├── Committer creates PR (not sandboxed)
   │     Creates branch via Git Data API
   │     Commits files from State
   │     Opens PR with plan + review in body
   │
   ├── Phase: Review (Ollama 3-pass + arbiter via configurable endpoint)
   │     Three passes: correctness, security, intent
   │     Arbiter classifies findings → fix_here/subtask/root_cause/dismissed
   │     Output: review findings + arbiter result → State
   │
   ├── Phase: Iterate (applies review feedback, loops to review)
   │
   └── Label fabriquilla:done
```

Each phase receives outputs from all prior phases via `State`.

### Planned: Post-PR feedback loop

After the committer creates a PR, a feedback loop would:
1. Trigger external review (Qodo)
2. Run factory review (DeepSeek)
3. Arbiter synthesizes all findings
4. Iterator applies fixes, committer pushes new commits
5. Loop until clean or max iterations reached

### Planned: Post-merge monitoring

Watch CI on main for 30 minutes after merge. If CI breaks: create revert PR
automatically, create new issue with failure context.

---

## 8. Guardrails

All guardrails are enforced in deterministic code (dispatcher, committer).
No guardrail depends on LLM judgment.

### Iteration limits

| Guardrail | Default | Enforcement point |
|---|---|---|
| max_iterations | 3 | Review-iterate cycles per issue |
| max_phase_duration | 15m | Timeout per phase binary |
| max_phase_retries | 2 | Retry on timeout/signal kill (exponential backoff) |

### Scope limits

| Guardrail | Default | Enforcement point |
|---|---|---|
| max_files_changed | 20 | committer: refuse to commit if exceeded |
| max_pr_size_lines | 500 | committer: refuse if diff exceeds this |

### Cost governance

| Guardrail | Default | Enforcement point |
|---|---|---|
| max_cost_budget | 100,000 tokens | dispatcher: cumulative across all phases |
| max_issues_per_hour | 5 | dispatcher: rate limit on processing |
| max_issues_per_day | 20 | dispatcher: daily cap |

### Self-modification protection

The committer refuses to commit changes to these paths:

```
.github/workflows/*     CI/CD pipelines
CODEOWNERS               permission boundaries
.pr_agent.toml           review tool configuration
CONVENTIONS.md           agent instructions
ARCHITECTURE.md          system design docs
CLAUDE.md                agent context
.serena/*                MCP configuration
deploy/*                 k8s manifests and sandbox configs
```

Enforced by `pipeline.ValidateFiles()`. If any file matches a blocked
pattern, the committer labels `fabriquilla:needs-human`.

### Planned: Root cause limits

| Guardrail | Default | Enforcement point |
|---|---|---|
| max_root_cause_issues_per_pr | 3 | feedback binary |
| max_root_cause_depth | 1 | dispatcher: root cause issues cannot create root cause issues |

### Review deadlock prevention

If a finding was dismissed by the arbiter in iteration N and the same
finding reappears in iteration N+1: auto-dismiss without re-arbitration.
This is shipped in `review/arbiter.go`.

---

## 9. needs_info Cases

Any phase can signal that it needs human input. The dispatcher posts a
structured comment and labels the issue `fabriquilla:needs-info`.

### Cases by phase

**Planner:**
- Ambiguous requirements (multiple valid interpretations)
- Missing reproduction steps for bug reports
- Conflicting constraints (issue vs. ARCHITECTURE.md)

**Gatherer:**
- Referenced code doesn't exist in the codebase
- Multiple candidates for an ambiguous reference

**Designer:**
- Design choice requires human decision (REST vs. gRPC)
- Impact on public API contract

**Reviewer:**
- Security concern that can't be confidently dismissed
- Implementation matches the plan but the plan may have misinterpreted intent

**Committer:**
- PR touches files protected by CODEOWNERS

**Guardrail triggers:**
- Max iterations reached with unresolved critical findings
- PR scope exceeded limits

### Guideline for agents

> Return `needs_info` only when proceeding would likely produce **wrong**
> output, not just imperfect output. If a reasonable assumption can be made,
> state the assumption and proceed. The review phase catches bad assumptions.

---

## 10. Golden-Set Evaluation

### Test case structure

```
tests/golden/
  planner/
    case-001-simple-bug.json       happy path → outcome "plan"
    case-002-missing-info.json     ambiguous issue → outcome "needs_info"
    case-003-complex-task.json     large scope → outcome "decompose"
  coder/
    case-001-add-function.json     add new function with tests
    case-002-modify-existing.json  modify existing code
    case-003-injection-trap.json   prompt injection in issue body
  reviewer/
    case-001-clean-code.json       correct code → no critical findings
    case-002-planted-bug.json      code with intentional bug → finds it
    case-003-scope-creep.json      code that exceeds issue scope → flags it
  arbiter/
    case-001-dismiss-qodo.json     Qodo finding invalid for project context
    case-002-root-cause.json       finding is systemic, should create issue
    case-003-subtask.json          finding needs work within PR
```

### Assertion types

| Type | Description |
|---|---|
| outcome_equals | PlanResult.Outcome matches exactly |
| output_contains | Output text contains substring |
| output_not_contains | Output text must not contain substring |
| file_count_gte | Number of parsed files >= N |
| file_paths_include | Specific file path appears in output |
| severity_present | ReviewResult contains a specific severity tag |
| compiles | Output code compiles (`go build` in sandbox) |
| tests_pass | Output code passes tests (`go test` in sandbox) |

### When to run

- On every agent prompt change (system prompt in `agents/*.go`)
- Weekly cron for drift detection (model updates can change behavior)
- Before switching to a new model version

---

## 11. Model Assignments

| Phase | Model | Provider | Reasoning |
|---|---|---|---|
| Gatherer | qwen3:14b | Ollama (local) | Tool calling, read-only code exploration |
| Researcher | Gemini 2.5 Flash | Google API (free tier) | Broad external research |
| Planner | Configurable | OpenAI-compatible API | Supports Gemini, DeepSeek, etc. |
| Designer | qwen3:14b | Ollama (local) | Structured technical output |
| Coder | qwen3:14b | Ollama (local) | Tool calling, code generation |
| Reviewer | qwen3:14b (3-pass) | Ollama (local) | Adversarial 3-pass review |
| Arbiter | Configurable (e.g. DeepSeek) | OpenAI-compatible API | Classifies findings; different family than coder |
| Iterator | qwen3:14b | Ollama (local) | Apply fixes with Serena tools |

---

## 12. Deployment

### Minimum requirements

| Resource | Minimum | Recommended | Notes |
|---|---|---|---|
| RAM | 24 GB | 32+ GB | Ollama (~12 GB) + gateway (~512 MB) + sandbox (~4 GB) + OS |
| GPU VRAM | 10 GB | 12+ GB | For qwen3:14b via Ollama |
| CPU | 4 cores | 8+ cores | Ollama uses 2 cores during inference |
| Disk | 50 GB | 100+ GB | Models (~10 GB), sandbox images (~5 GB each), repo clones |
| OS | Linux (x86_64) | Fedora, Ubuntu, Debian | OpenShell requires Linux kernel |

### Infrastructure

```
Host machine
│
├── Ollama (systemd service, always running)
│     Local model loaded in VRAM
│     Listening on localhost:11434
│
├── OpenShell gateway (Docker container, always running)
│     Manages sandbox lifecycle, ~512MB RAM
│     SQLite backend (single-node)
│
├── fabriquilla-dispatcher (systemd service or k3s Deployment)
│     Long-running poll loop
│     Mounts /data/pipeline for state files
│     Contains all phase binaries in same image
│
└── OpenShell sandboxes (ephemeral containers)
      Created per-phase, destroyed after completion
      One active sandbox at a time (sequential pipeline)
```

### Container image

One fat image containing all binaries:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN for bin in dispatcher gatherer researcher planner designer coder \
    reviewer iterator committer eval eval-runner; do \
      CGO_ENABLED=0 go build -o /out/$bin ./cmd/$bin/; \
    done

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y git python3 python3-pip \
    && rm -rf /var/lib/apt/lists/*
RUN pip3 install serena
COPY --from=build /out/* /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/dispatcher"]
```

---

## 13. Execution Layers

Following [fullsend](https://github.com/fullsend-ai/fullsend) patterns,
the system separates concerns into layers:

- **Dispatch** (`cmd/dispatcher/`) — poll loop, repo iteration, label state machine
- **Infrastructure** (`github/`, `inference/`, `gemini/`) — API clients, authentication, credential management
- **Sandbox** (`sandbox/`, `openshell/`) — input sanitization, sandbox isolation
- **Harness** (`harness/`) — assembles phase context from repo docs and prior phase outputs
- **Runtime** (`agents/`) — LLM prompts and response parsing, no business logic
- **Pipeline** (`pipeline/`) — state serialization, guardrails, file validation

---

## 14. Authentication

Two auth modes, selected per repo in config:

- **GitHub App** (preferred) — `AppAuth` generates RS256 JWTs from the app's private key, exchanges them for installation access tokens via GitHub API, caches tokens with 5-minute pre-expiry refresh
- **PAT** (fallback) — static token wrapped in `staticToken` implementing `TokenSource`

Both implement `TokenSource` so `Client` is auth-agnostic.

### Credential isolation

Credentials never appear in config files, logs, or agent context:

- `GITHUB_APP_PRIVATE_KEY_PATH` env var → PEM file path (k8s Secret volume mount)
- `GEMINI_API_KEY` env var → API key (k8s Secret)
- Config file (ConfigMap) holds only non-secret settings

---

## 15. Label State Machine

Issues move through states via GitHub labels:

```
fabriquilla:ready → fabriquilla:in-progress → fabriquilla:done
                                   → fabriquilla:needs-info (awaiting human)
                                   → fabriquilla:needs-human (stuck)
                                   → fabriquilla:tracking (decomposed into sub-issues)
```

---

## 16. Key Interfaces

- `github.TokenSource` — `Token(ctx) (string, error)` — implemented by `staticToken` and `AppAuth`
- `inference.ToolHandler` — `Execute(ctx, name, args) (string, error)` — implemented by `mcp.Client`, `ContextToolHandler`, `CompositeToolHandler`
- `pipeline.StateStore` — `Save`/`Load` for pipeline state — implemented by `FileStateStore`

---

## Planned Features (Not Yet Implemented)

These items from the original v2 design are not yet in the codebase:

1. **Human review adapter** (`review/` package) — parse GitHub PR review comments into `ReviewFinding` via the shipped `ExternalReviewAdapter` interface
2. **Feedback binary** (`cmd/feedback/`) — post-PR review loop and structured feedback capture
3. **Dashboard** (`cmd/dashboard/`) — web UI for config, monitoring, reports, control
4. **Self-improvement feedback loop** — structured JSONL logging of review outcomes, periodic analysis
5. **Scoped installation tokens** — `TokenWithPermissions()` on `AppAuth`
6. **Post-merge monitoring** — CI watching, automatic revert PR creation
7. **Merge automation** — committer checks status checks and merges on approval

---

## Design Principles

1. **Security is the foundation, not a layer.** Every component designed
   with adversarial thinking. Sandbox isolation, scoped credentials, blocked
   paths.
2. **Autonomy is earned, not granted.** Repos graduate from shadow mode
   based on demonstrated safety.
3. **Deterministic where possible.** Guardrails, file validation, merge
   gates — all in Go code, never in LLM judgment.
4. **Zero framework cognition.** The orchestrator handles mechanics. All
   judgment is deferred to LLMs via prompts.
5. **Pluggable external review.** The external reviewer is behind an
   interface. Swapping tools requires one adapter, not a rewrite.
6. **Agents communicate through forge artifacts.** Issues, PRs, comments,
   labels, status checks. No side channels, no agent-to-agent API.
7. **Review is harder than generation.** The arbiter role uses a stronger
   model than the generation roles.

---

## Roadmap

Migration from monolithic factory-orchestrator to multi-app sandboxed
architecture with scoped GitHub App identities, OpenShell sandboxing,
pluggable review, self-improvement feedback loop, comprehensive guardrails,
and budget control.

### v2.1 — Budget & Resilience (complete)

- [x] #27 — Extract shared packages
- [x] #28 — Split into separate binaries
- [x] #30 — Pipeline state serialization
- [x] #50 — Instrument API clients for token counting
- [x] #51 — Retry with backoff + stall detection
- [x] #52 — Structured output for coder phase
- [x] #36 — Guardrails in dispatcher and committer

### v2.2 — Sandboxed Execution (complete)

- [x] #29 — Three GitHub Apps
- [x] #34 — OpenShell sandbox integration (coder-first MVP)
- [x] #35 — Sandbox images (go + rust + typescript)
- [x] #53 — MCP credential redaction
- [x] #54 — SSRF protection for URL-capable tools

### v2.3 — Review & Verification (in progress)

- [x] #31 — Review adapter interface + QodoAdapter
- [x] #32 — Arbiter phase via OpenAI-compatible API
- [x] #65 — Review-iterate loop wired into dispatcher
- [ ] #33 — Feedback binary with structured logging
- [ ] #39 — Human review adapter + merge automation (deferred until eval evidence)

### Testing & Validation (in progress)

- [x] #74 — Prompt injection adversarial corpus
- [x] #76 — Golden-set eval cases for all phases
- [x] #78 — Security boundary unit tests
- [ ] #73 — Wire eval framework to real inference
- [ ] #79 — Contract tests with recorded fixtures
- [ ] #80 — Trace audit for credential leakage
- [ ] #77 — End-to-end smoke test

### v2.5 — Agent Quality (open)

Smarter cognition inside existing privilege boundaries — no sandbox,
credential, or network-policy changes.

- [ ] #87 — Designer phase: read-only code navigation tools
- [ ] #88 — `plan_infeasible` coder outcome with bounded re-plan loop
- [ ] #89 — Per-repo coder model configuration

### v2.4 — Observability & Feedback Loop (open)

- [ ] #55 — Persistent run history (append-only JSONL store)
- [ ] #56 — Context7 as second MCP server
- [ ] #38 — Post-merge monitoring + auto-revert (depends on #33)
- [ ] #40 — Dashboard: config, monitoring, reports (depends on #33, #55)
