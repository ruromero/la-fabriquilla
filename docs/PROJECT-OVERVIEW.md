# La Fabriquilla - Project Overview

## What Is This?

La Fabriquilla is an autonomous software development orchestrator. It polls
GitHub repositories for issues tagged `fabriquilla:ready`, drives each issue
through a multi-phase LLM pipeline, and opens pull requests with the results.
The system is written entirely in Go (stdlib only, zero external dependencies)
and runs on commodity hardware with a single GPU.

The name comes from the Go module: `github.com/ruromero/la-fabriquilla`.

---

## Core Philosophy

Four principles shape every design decision:

1. **Zero framework cognition** - The orchestrator handles mechanics (polling,
   state, file I/O, sandboxing). All judgment is deferred to LLMs via prompts.
   No LangChain, no CrewAI, no agent frameworks.

2. **Security is the foundation, not a layer** - Adversarial thinking from the
   start. Sandbox isolation, scoped credentials, blocked paths, input
   sanitization, secret redaction. Every component assumes the LLM might be
   manipulated.

3. **Deterministic where possible** - Guardrails, file validation, merge gates,
   cost limits, rate limits, and scope checks are all in Go code. Never in LLM
   judgment.

4. **Review is harder than generation** - The arbiter/review role uses a
   stronger model than the generation roles. The review model must be a
   different family than the coder model.

---

## Pipeline Flow

```
 Human creates GitHub issue
 Adds label "fabriquilla:ready"
         │
         ▼
 ┌──────────────────────────────────────────────┐
 │  DISPATCHER (long-running poll loop)          │
 │                                               │
 │  1. Poll GitHub for fabriquilla:ready issues  │
 │  2. Check repo readiness (required files)     │
 │  3. Swap label: ready → in-progress           │
 │  4. Sanitize issue content (Unicode, secrets) │
 │  5. Load repo context (README, ARCH, CONV)    │
 │  6. Initialize pipeline State                 │
 │  7. Clone repo + start Serena MCP server      │
 │                                               │
 │  Then run phases sequentially:                │
 └──────────────┬───────────────────────────────┘
                │
    ┌───────────▼───────────┐
    │   GATHER              │
    │   Model: qwen3:14b    │
    │   Tools: Serena (R/O) │
    │   Output: codebase    │
    │   context map         │
    └───────────┬───────────┘
                │
    ┌───────────▼───────────┐
    │   RESEARCH            │
    │   Model: Gemini 2.5   │
    │   Flash               │
    │   Tools: none          │
    │   Output: library     │
    │   docs, patterns      │
    │   (optional phase)    │
    └───────────┬───────────┘
                │
    ┌───────────▼───────────┐
    │   PLAN                │
    │   Model: configurable │
    │   (OpenAI-compatible) │
    │   Tools: none          │
    │   Output: one of:     │
    │   • plan → continue   │
    │   • needs_info → stop │
    │   • decompose → stop  │
    └───────────┬───────────┘
                │ (only if outcome = "plan")
    ┌───────────▼───────────┐
    │   DESIGN              │
    │   Model: qwen3:14b    │
    │   Tools: none          │
    │   Output: API         │
    │   contracts, data     │
    │   models, file layout │
    └───────────┬───────────┘
                │
    ┌───────────▼───────────┐
    │   CODE                │
    │   Model: qwen3:14b    │
    │   Tools: Serena (R/W) │
    │   Output: complete    │
    │   file contents       │
    └───────────┬───────────┘
                │
    ┌───────────▼───────────┐
    │   COMMIT              │
    │   (deterministic,     │
    │    no LLM)            │
    │   Creates branch,     │
    │   commit, and PR      │
    │   via GitHub API      │
    └───────────┬───────────┘
                │
    ┌───────────▼──────────────────────────┐
    │   REVIEW ↔ ITERATE LOOP             │
    │                                      │
    │   Review (3 passes):                 │
    │   • Correctness (tools: Serena R/O)  │
    │   • Security    (tools: Serena R/O)  │
    │   • Intent      (tools: none)        │
    │                                      │
    │   If [CRITICAL] or [MEDIUM] found:   │
    │   → Iterator applies fixes           │
    │   → Re-review                        │
    │   → Loop up to max_iterations times  │
    └───────────┬──────────────────────────┘
                │
    ┌───────────▼───────────┐
    │   Label: done         │
    │   PR ready for human  │
    │   review and merge    │
    └───────────────────────┘
```

### Planner Decision Points

The planner can produce three outcomes:

| Outcome | What Happens |
|---------|-------------|
| `plan` | Continue to design → code → commit → review |
| `needs_info` | Post questions as issue comment, label `fabriquilla:needs-info`, stop |
| `decompose` | Create sub-issues with `fabriquilla:ready`, label parent `fabriquilla:tracking`, stop |

### Shadow Mode

When `shadow_mode: true` (the default), the committer posts generated code as
an issue comment instead of creating a real PR. This lets you validate the
pipeline without any write operations to the repo.

---

## Binary Layout

The project compiles into multiple binaries from a single Go module. The
dispatcher is long-running; all phase binaries are one-shot (invoked as
subprocesses by the dispatcher).

```
cmd/
  dispatcher/    long-running: poll loop, phase orchestration
  gatherer/      one-shot: context gathering via LLM + Serena
  researcher/    one-shot: external research via configurable model
  planner/       one-shot: planning via OpenAI-compatible API
  designer/      one-shot: technical design via LLM
  coder/         one-shot: code generation via LLM + Serena
  committer/     one-shot: branch, commit, PR creation
  reviewer/      one-shot: 3-pass code review + arbiter classification
  iterator/      one-shot: apply review feedback via LLM + Serena
  feedback/      one-shot: post-PR review loop, collects external findings
  eval/          golden-set evaluation harness
  eval-runner/   model comparison eval runner (name@endpoint syntax)
  internal/      shared helpers (config/state loading, GitHub client)
```

### Communication Between Binaries

Phases communicate exclusively through a shared JSON state file on disk:

```
/data/pipeline/{owner}/{repo}/{issue_number}.json
```

Each phase binary reads the state, does its work, updates the state, and saves.
The dispatcher reloads state after each phase to make decisions (e.g., checking
the plan outcome, enforcing cost budgets).

Environment variables tell each binary where to find its inputs:
- `PIPELINE_STATE_PATH` - path to the state JSON file
- `CONFIG_PATH` - path to the config JSON file

---

## Package Map

```
┌─────────────────────────────────────────────────────────────┐
│                      cmd/ (binaries)                        │
│  dispatcher  gatherer  researcher  planner  designer        │
│  coder  committer  reviewer  iterator  eval  eval-runner     │
│  internal/helpers.go (shared bootstrap)                     │
└──────────────┬──────────────────────────────────────────────┘
               │ imports
┌──────────────▼──────────────────────────────────────────────┐
│                    Shared Packages                           │
│                                                              │
│  config/      Config loading, validation, defaults           │
│  pipeline/    State struct, persistence, parsing, guardrails │
│  agents/      Pure functions: one per pipeline phase         │
│  harness/     Context assembly, tool routing, Serena mgmt    │
│  inference/   OpenAI-compatible HTTP client + tool loop      │
│  github/      GitHub REST API, App auth, readiness checks    │
│  mcp/         MCP client (JSON-RPC 2.0 over stdio)           │
│  sandbox/     Input sanitization, secrets, SSRF protection   │
│  openshell/   OpenShell sandbox lifecycle                    │
│  traces/      Structured JSON telemetry                     │
│  eval/        Golden-set test runner and reporting           │
└──────────────────────────────────────────────────────────────┘
```

### Package Dependency Graph

```
agents/  ←── depends on ──→  inference/ (client, types)

harness/ ←── depends on ──→  inference/ (Tool, ToolHandler)
                              mcp/      (MCP client)
                              github/   (file access, cloning)
                              sandbox/  (secret redaction)
                              config/   (SerenaConfig)

cmd/*    ←── depends on ──→  config/    (LoadConfig)
                              pipeline/  (State, Store)
                              agents/    (phase functions)
                              harness/   (tools, Serena)
                              github/    (API client)
                              inference/ (LLM client)
                              sandbox/   (SanitizeInput)

pipeline/ ←── depends on ──→ sandbox/   (secret patterns)
```

---

## Agents in Detail

Each agent is a pure function in `agents/*.go`. They take context data and
return structured results. No agent has side effects or accesses the filesystem
directly.

### 1. Gatherer (`agents/gatherer.go`)

**Role:** Explore the codebase to understand existing code relevant to the issue.

**Input:** Issue title/body, project summaries, 7 code navigation tools
(list_dir, search_for_pattern, read_file, find_symbol, etc.)

**Output:** Three-part context: EXISTING CODE (file paths, functions), GAPS
(what's missing), PATTERNS (implementation examples from the codebase).

**Behavior:** Up to 25 tool calls. Temperature 0. Told to be thorough and not
stop after a few failed lookups.

### 2. Researcher (`agents/researcher.go`)

**Role:** Find external documentation, library patterns, and best practices.

**Input:** Issue title/body, tech stack context, inference client.

**Output:** Concise reference document with actionable patterns and pitfalls.

**Behavior:** Configurable model via any OpenAI-compatible API (e.g. Gemini 2.5 Flash). No tools. Optional phase - gracefully
returns empty if no researcher model is configured.

### 3. Planner (`agents/planner.go`)

**Role:** Produce an implementation plan, request info, or decompose the issue.

**Input:** Issue, gathered context, research context, conventions, comment
history. Model name is configurable (supports any OpenAI-compatible API).

**Output:** One of three outcomes (plan, needs_info, decompose). Parses the
first line of output to classify.

**Behavior:** No tools. Temperature 0. Emphasis on planning the DELTA (what
changes), not re-implementing what exists. Prefers assumptions over asking
questions.

### 4. Designer (`agents/designer.go`)

**Role:** Produce a technical design document from the plan.

**Input:** Plan, research context, conventions.

**Output:** Structured markdown: API contracts, data models, component
boundaries, file structure, dependencies.

**Behavior:** qwen3:14b. No tools. Temperature 0.

### 5. Coder (`agents/coder.go`)

**Role:** Write the implementation code.

**Input:** Design, research context, conventions. Optional Serena tools for
read+write file operations.

**Output:** Complete file contents in JSON format
(`{files: [{path, language, content}]}`). Fallback: markdown code blocks
with `FILE:` headers.

**Behavior:** qwen3:14b. Up to 20 tool calls. Temperature 0. Two modes:
with tools (agentic via ChatWithTools) or without tools (structured JSON
output via schema).

### 6. Reviewer (`agents/reviewer.go`)

**Role:** Adversarial code review across three independent dimensions.

**Three Review Passes:**
- **Correctness** - Logic errors, edge cases, error handling, test coverage.
  Uses tools (10 calls max).
- **Security** - Injections, auth bypasses, data exposure. CWE categorization.
  Uses tools (10 calls max).
- **Intent** - Issue alignment, scope creep, completeness vs plan. No tools.

**Output:** Severity tags per finding: `[CRITICAL]`, `[MEDIUM]`, `[LOW]`,
`[PASS]`, `[ALIGNED]`, `[SCOPE_CREEP]`, `[MISSING]`.

**Behavior:** qwen3:14b. Temperature 0. Prompt tells reviewers to "find
problems, not approve code."

### 7. Arbiter (`agents/arbiter.go`)

**Role:** Classify review findings from all sources into actions.

**Input:** Review findings, conventions, project summaries, plan, and the
fingerprints of previously dismissed findings.

**Output:** Each finding classified as `fix_here`, `subtask`, `root_cause`,
or `dismissed` (with reason). Previously dismissed findings that reappear
are auto-dismissed to prevent review deadlock.

**Behavior:** Runs on a separate OpenAI-compatible endpoint configured
via `arbiter.model` (e.g. `deepseek-chat@deepseek`).
Optional — if disabled or failing, review falls back to severity-based
handling.

### 8. Iterator (`agents/iterator.go`)

**Role:** Fix code based on review feedback.

**Input:** Current code, review findings with severity levels.

**Output:** Fixed code with CRITICAL issues resolved first, then MEDIUM.

**Behavior:** Same model and output format as Coder. Explicitly prohibited
from introducing new features - only fix the issues raised.

---

## Inference Architecture

### OpenAI-Compatible Client (`inference/client.go`)

All LLM interaction goes through a single HTTP client that speaks the OpenAI
chat completions API. This works with Ollama, DeepSeek, Gemini (via OpenAI
compatibility layer), and any other provider.

**Key types:**
- `Client` - HTTP client with base URL and optional API key
- `ChatRequest` - model, messages, tools, temperature, response format
- `Tool` / `ToolCall` - function calling definitions and invocations
- `Usage` - prompt/completion token counts

**Key methods:**
- `Chat()` - single API call
- `ChatWithTools()` - agentic tool-calling loop (call model → execute tools →
  append results → repeat until done or max calls reached)
- `SimpleChat()` - convenience wrapper for single-turn no-tools
- `StructuredOutput()` - creates JSON schema response format

### Tool-Calling Loop

The `ChatWithTools` loop is the core agentic pattern:

```
1. Send messages + tool definitions to model
2. Model responds with tool_calls (or final text)
3. For each tool_call:
   a. Route to handler via CompositeToolHandler
   b. Execute tool, get result string
   c. Append tool result as message
4. Go to step 1
5. Stop when model returns text (no tool_calls) or max calls exceeded
```

Token usage accumulates across all iterations of the loop.

---

## MCP Integration

### Serena (LSP Tools)

Serena is an MCP server that exposes Language Server Protocol capabilities
as tools. The harness manages its lifecycle:

1. Clone the target repo (shallow, via GitHub API)
2. Install language servers (gopls, rust-analyzer, typescript-language-server)
3. Start Serena as a subprocess
4. Connect via JSON-RPC 2.0 over stdio
5. Discover available tools via `tools/list`
6. Execute tool calls via `tools/call`

**Tool filtering by role:**

| Role | Allowed Serena Tools |
|------|---------------------|
| Gatherer (read-only) | find_symbol, find_referencing_symbols, find_referencing_code_snippets, get_symbols_overview, read_file, list_dir, search_for_pattern |
| Coder (read+write) | All gather tools + replace_symbol_body, insert_before_symbol, insert_after_symbol, replace_content |

### Context Tools

In addition to Serena, the harness provides documentation tools:

- `list_documents` - Lists loaded doc files (README, ARCHITECTURE, CONVENTIONS)
- `list_sections` - Lists markdown sections in a document
- `get_section` - Gets specific section content
- `get_document` - Gets full document
- `read_file` - Reads source files from repo via GitHub API
- `list_files` - Lists directory contents via GitHub API

### CompositeToolHandler

Routes tool calls to the correct handler by name. Serena tools go to the MCP
client; documentation tools go to the ContextToolHandler. Also applies secret
redaction to all tool outputs before returning to the LLM.

---

## Pipeline State

### State Struct (`pipeline/state.go`)

All inter-phase data lives in a single JSON-serializable struct:

```go
type State struct {
    // Identity
    RepoOwner, RepoName string
    IssueNumber         int

    // Pipeline position
    Phase     string
    Iteration int

    // Input (from GitHub)
    IssueTitle, IssueBody string
    CommentHistory        string
    Summaries, Conventions string

    // Phase outputs
    GatheredContext string
    ResearchContext string
    PlanOutcome     string        // "plan", "needs_info", "decompose"
    PlanContent     string
    Design          string
    Code            string
    Review          *ReviewState
    Files           []FileState   // Parsed {path, content} pairs

    // PR tracking
    PRNumber int
    PRBranch string

    // Token accounting
    TokenUsage     []TokenUsage
    TotalTokens    int
    EstimatedCost  float64

    // Timing
    StartedAt, UpdatedAt time.Time
    CloneDir             string
}
```

State is saved atomically (write temp file, then rename) to prevent corruption
if a phase crashes.

### Token Usage Tracking

Every phase records per-call token metrics:

```go
type TokenUsage struct {
    Phase            string
    Model            string
    PromptTokens     int
    CompletionTokens int
    ToolCalls        int
    EstimatedCostUSD float64
    WallTime         time.Duration
}
```

Cost estimation uses hardcoded per-model rates. Ollama (local) models are free.
Cumulative cost is checked after every phase via `CheckCostBudget()`.

---

## Security Layers

### Input Sanitization (`sandbox/sanitize.go`)

All untrusted input (issue titles, bodies, comments) is sanitized before
entering any LLM context:

- Strip Unicode tag characters (U+E0000-U+E007F) - steganographic injection
- Strip zero-width characters (U+200B-U+200D, U+FEFF)
- Strip bidirectional overrides (U+202A-U+202E) - text direction attacks
- Strip bidirectional isolates (U+2066-U+2069)
- Strip other control characters (except tab, CR, LF)

### Secret Detection (`sandbox/secrets.go`)

10 regex patterns detect credentials in text:

- PEM private key blocks
- API keys (generic patterns)
- Bearer tokens
- AWS access keys (`AKIA...`)
- GitHub tokens (`ghp_`, `gho_`, `ghs_`, `ghu_`, `github_pat_`)
- Database connection strings
- Password fields in config
- Generic `SECRET`/`TOKEN`/`PASSWORD` env vars
- IPv4 addresses and hostnames (to prevent network exposure)

Two uses:
1. **Redaction** - `RedactSecrets()` replaces matches with `[REDACTED]` in MCP
   tool outputs before they enter LLM context
2. **Validation** - `ValidateContents()` scans generated code for embedded
   secrets before committing

### SSRF Protection (`sandbox/url.go`)

Prevents agent-generated requests from hitting internal infrastructure:

- Validates URL scheme (http/https only)
- Resolves DNS and checks IPs against blocked CIDRs (10.0.0.0/8,
  172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, etc.)
- Blocks cloud metadata endpoints (169.254.169.254, metadata.google.internal)
- Re-validates on redirects to prevent DNS rebinding
- Fails closed: DNS resolution errors reject the URL

### Path Validation (`pipeline/validate.go`)

Before committing, all file paths are validated:

- Reject path traversal (`../`)
- Reject absolute paths
- Reject files matching blocked patterns (configurable via `blocked_paths`)

### Blocked Paths (Self-Modification Protection)

The committer refuses to modify these paths:

```
.github/workflows/*    CI/CD pipelines
CODEOWNERS             permission boundaries
.pr_agent.toml         review tool config
CONVENTIONS.md         agent instructions
ARCHITECTURE.md        system design docs
CLAUDE.md              agent context
.serena/*              MCP configuration
deploy/*               k8s manifests, sandbox configs
```

If any file matches, the issue gets labeled `fabriquilla:needs-human`.

### Sandbox Isolation (`openshell/`)

Phase binaries can run inside OpenShell sandboxed containers with per-phase
network policies:

| Phase | Network Access |
|-------|---------------|
| gatherer | Ollama only |
| researcher | generativelanguage.googleapis.com only |
| planner | configured planner endpoint only |
| designer | Ollama only |
| coder | Ollama only |
| reviewer | Ollama + configured arbiter endpoint only |
| iterator | Ollama only |
| dispatcher | Not sandboxed (trusted code, needs GitHub API) |
| committer | Not sandboxed (trusted code, needs GitHub write) |

The coder sandbox has NO access to `api.github.com`. Even if the LLM is
manipulated via prompt injection, it physically cannot push code.

### GitHub App Credential Scoping

Three separate GitHub Apps aligned to trust boundaries:

| App | Used By | Permissions |
|-----|---------|------------|
| fabriquilla-dispatcher | dispatcher | issues:write, contents:read |
| fabriquilla-worker | gatherer, coder | contents:read only |
| fabriquilla-committer | committer | contents:write, pull_requests:write, issues:write |

Credentials are loaded from env vars, never from config files or LLM context.
The committer is the ONLY identity that can push code.

---

## Guardrails

All enforced in deterministic Go code. No guardrail depends on LLM judgment.

### Iteration Limits

| Guardrail | Default | Enforcement |
|-----------|---------|-------------|
| max_iterations | 3 | Review-iterate cycles per issue |
| max_phase_duration | 15m | Timeout per phase binary |
| max_phase_retries | 2 | Retry on timeout/signal kill (exponential backoff) |

### Scope Limits

| Guardrail | Default | Enforcement |
|-----------|---------|-------------|
| max_files_changed | 20 | Reject PR if exceeded |
| max_pr_size_lines | 500 | Reject PR if diff exceeds this |

### Cost Governance

| Guardrail | Default | Enforcement |
|-----------|---------|-------------|
| max_cost_budget | 100,000 tokens | Cumulative across all phases |
| max_issues_per_hour | 5 | Dispatcher rate limit |
| max_issues_per_day | 20 | Dispatcher daily cap |

### Phase Retry Logic

Phases are retried on timeout or signal kill (OOM). Non-retryable failures
(normal exit codes) fail immediately. The committer and iterator are never
retried because they have non-idempotent side effects.

Backoff: 30s, 60s, 120s (capped at 2 minutes).

---

## GitHub Integration

### Label State Machine

```
fabriquilla:ready
    │
    ▼
fabriquilla:in-progress ──→ fabriquilla:done
    │                             (PR opened)
    ├──→ fabriquilla:needs-info   (awaiting human input)
    ├──→ fabriquilla:needs-human  (pipeline failed/stuck)
    ├──→ fabriquilla:tracking     (decomposed into sub-issues)
    └──→ fabriquilla:requirements (repo missing required files)
```

### Repo Readiness

Before processing any issue, the dispatcher checks for required files:

- `README.md`
- `ARCHITECTURE.md`
- `CONVENTIONS.md`
- `CODEOWNERS` (checked in root, `.github/`, and `docs/`)
- `CLAUDE.md`
- `.serena/` directory

Missing files cause: label swapped to `fabriquilla:requirements`, comment
posted explaining what's missing, issue not processed.

### PR Creation

The committer creates PRs via the GitHub Git Data API (not git push):

1. Get branch SHA from default branch
2. Create branch ref
3. Create tree with file contents
4. Create commit pointing to tree
5. Update branch ref to new commit
6. Create PR from branch to default branch

This avoids needing git installed or SSH keys in the runtime environment.

---

## Configuration

Single JSON file with defaults applied at load time:

All inference endpoints are defined in a shared `endpoints` registry. Models
reference endpoints with `name@endpoint` syntax (e.g. `qwen2.5-coder:14b@ollama`).

```json
{
  "default_model": "qwen2.5-coder:14b@ollama",
  "planner": {"model": "gemini-2.5-flash@gemini"},
  "researcher": {"model": "gemini-2.5-flash@gemini"},
  "arbiter": {"model": "deepseek-chat@deepseek"},
  "endpoints": {
    "ollama": {"base_url": "http://ollama.ai.svc.cluster.local:11434/v1"},
    "gemini": {"base_url": "https://generativelanguage.googleapis.com/v1beta/openai", "api_key_env": "GEMINI_API_KEY"},
    "deepseek": {"base_url": "https://api.deepseek.com/v1", "api_key_env": "DEEPSEEK_API_KEY"}
  },
  "poll_interval": "30s",
  "max_iterations": 3,
  "max_cost_budget": 100000,
  "max_phase_duration": "15m",
  "max_phase_retries": 2,
  "shadow_mode": true,
  "max_files_changed": 20,
  "max_pr_size_lines": 500,
  "max_issues_per_hour": 5,
  "max_issues_per_day": 20,
  "serena": {
    "command": "serena-agent",
    "args": ["--project-path", "."]
  },
  "sandbox": {
    "enabled": false,
    "policy_dir": "deploy/sandbox-policies",
    "image": "factory-base:latest"
  },
  "apps": {
    "dispatcher": {"app_id": 111, "installation_id": 222},
    "worker": {"app_id": 333, "installation_id": 444},
    "committer": {"app_id": 555, "installation_id": 666}
  },
  "repos": [
    {"owner": "ruromero", "repo": "example-repo"}
  ]
}
```

Credentials are loaded from env vars referenced by `api_key_env` in endpoint configs:

| Env Var | Purpose |
|---------|---------|
| `GEMINI_API_KEY` | Gemini API key (referenced by endpoint config) |
| `DEEPSEEK_API_KEY` | DeepSeek API key (referenced by endpoint config) |
| `GITHUB_APP_PRIVATE_KEY_PATH` | Fallback PEM path for all apps |
| `FABRIQUILLA_DISPATCHER_KEY_PATH` | Dispatcher app PEM |
| `FABRIQUILLA_WORKER_KEY_PATH` | Worker app PEM |
| `FABRIQUILLA_COMMITTER_KEY_PATH` | Committer app PEM |

---

## Model Assignments

| Phase | Model | Provider | Why |
|-------|-------|----------|-----|
| Gatherer | qwen3:14b | Ollama (local) | Tool calling, read-only exploration |
| Researcher | Configurable (e.g. Gemini 2.5 Flash) | Any OpenAI-compatible | Broad external research |
| Planner | Configurable | Any OpenAI-compatible | Supports Gemini, DeepSeek, etc. |
| Designer | qwen3:14b | Ollama (local) | Structured technical output |
| Coder | qwen3:14b | Ollama (local) | Tool calling, code generation |
| Reviewer | qwen3:14b | Ollama (local) | Adversarial 3-pass review |
| Arbiter | Configurable (DeepSeek intended) | OpenAI-compatible API | Classifies findings; must be a different family than the coder |
| Iterator | qwen3:14b | Ollama (local) | Apply fixes with Serena tools |

All local models run through Ollama on a single consumer GPU. Only one
inference runs at a time (sequential pipeline).

---

## Deployment

### Minimum Hardware

| Resource | Minimum | Notes |
|----------|---------|-------|
| RAM | 24 GB | Ollama ~12GB + orchestrator + sandbox |
| GPU VRAM | 10 GB | qwen3:14b via Ollama |
| CPU | 4 cores | Ollama uses 2 during inference |
| Disk | 50 GB | Models ~10GB, sandbox images ~5GB each |
| OS | Linux x86_64 | OpenShell requires Linux kernel |

### Infrastructure Layout

```
Host machine
├── Ollama (systemd, always running, localhost:11434)
├── OpenShell gateway (Docker, manages sandboxes)
├── fabriquilla-dispatcher (systemd or k3s Deployment)
│   Mounts /data/pipeline for state files
│   Contains all phase binaries
└── OpenShell sandboxes (ephemeral, per-phase)
    One active at a time (sequential pipeline)
```

### Container Images

Sandbox images live in `deploy/sandbox-images/` — a common base extended
per language with appropriate toolchains and LSP servers:

- `base/Dockerfile` — multi-stage Go build, all phase binaries, git, Serena
- `go/Dockerfile` — extends base with Go toolchain + gopls
- `rust/Dockerfile` — extends base with Rust toolchain + rust-analyzer
- `typescript/Dockerfile` — extends base with Node.js + typescript-language-server

CI builds and pushes these as `ghcr.io/ruromero/factory-{base,go,rust,typescript}`.

### Kubernetes

```yaml
# Credentials as Secret (never in ConfigMap)
kubectl create secret generic fabriquilla-creds \
  --from-file=github-app.pem=/path/to/key.pem \
  --from-literal=GEMINI_API_KEY=your-key \
  --from-literal=DEEPSEEK_API_KEY=your-key

# Config as ConfigMap
# PEM mounted as volume, API keys as env vars
```

---

## Evaluation Framework

### Golden-Set Testing (`eval/`)

Test cases live in `tests/golden/{phase}/case-*.json`. Each case defines
inputs, expected assertions, and a pass threshold.

**Assertion types:**

| Type | Description |
|------|-------------|
| outcome_equals | PlanResult.Outcome matches exactly |
| output_contains | Output text contains substring |
| output_not_contains | Output must not contain substring |
| file_count_gte | Number of parsed files >= N |
| file_paths_include | Specific file path appears in output |
| severity_present | Review contains [CRITICAL] or [MEDIUM] |
| compiles | Output code compiles (requires sandbox) |
| tests_pass | Output code passes tests (requires sandbox) |

**When to run:**
- Every agent prompt change
- Weekly cron for drift detection
- Before switching model versions

---

## Open Issues and Roadmap

### v2.1 - Budget & Resilience (complete)

Completed:
- [x] Extract shared packages
- [x] Split into separate binaries
- [x] Pipeline state serialization
- [x] Token counting instrumentation
- [x] Retry with backoff + stall detection
- [x] Structured output for coder phase
- [x] Guardrails in dispatcher and committer

### v2.2 - Sandboxed Execution (complete)

Completed:
- [x] Three GitHub Apps
- [x] OpenShell sandbox integration (coder-first MVP)
- [x] Sandbox images (Go, Rust, TypeScript)
- [x] MCP credential redaction
- [x] SSRF protection

### v2.3 - Review & Verification (in progress)

| Issue | Description | Status |
|-------|-------------|--------|
| #31 | Review adapter interface + QodoAdapter | ✅ Done |
| #32 | Arbiter phase via OpenAI-compatible API | ✅ Done |
| #65 | Review-iterate loop wired into dispatcher | ✅ Done |
| #33 | Feedback binary with structured logging | Open, **priority: now** |
| #39 | Auto-merge gate + human escalation | Deferred until eval/smoke evidence |

Shipped so far: `ExternalReviewAdapter` interface, `QodoAdapter`, arbiter
classification (fix_here/subtask/root_cause/dismissed) with dismissed-finding
deadlock prevention. Remaining: feedback binary (post-PR review loop),
`HumanAdapter`, and the risk-based auto-merge gate (opt-in per repo,
requires a CI-quality bar).

### Testing & Validation (in progress)

| Issue | Description | Status |
|-------|-------------|--------|
| #74 | Prompt injection adversarial corpus | ✅ Done |
| #76 | Golden-set eval cases for all phases | ✅ Done |
| #78 | Security boundary unit tests | ✅ Done |
| #73 | Wire eval framework to real inference | Open, **priority: now** |
| #79 | Contract tests with recorded fixtures | Open |
| #80 | Trace audit for credential leakage | Open |
| #77 | End-to-end smoke test | Open |
| #81 | Self-hosted GPU runner for eval CI | Open, after #73 |
| #75 | LLM-as-judge assertion type | Open, after #73 |

This track gates further autonomy investment: auto-merge (#39) and the
dashboard (#40) stay deferred until evals and the smoke test show the
pipeline reliably produces mergeable PRs.

### v2.5 - Agent Quality (open)

Smarter cognition inside existing privilege boundaries — no sandbox,
credential, or network-policy changes. Each item is validated against the
eval harness (#73) before becoming default behavior.

| Issue | Description |
|-------|-------------|
| #87 | Designer phase: read-only code navigation tools |
| #88 | `plan_infeasible` coder outcome with bounded re-plan loop |
| #89 | Per-repo coder model configuration (local Ollama or frontier API) |

### v2.4 - Observability & Feedback Loop (open)

| Issue | Description | Status |
|-------|-------------|--------|
| #55 | Persistent run history (append-only JSONL store) | Open |
| #56 | Add Context7 as second MCP server | Open |
| #38 | Post-merge monitoring + automatic revert | Open, depends on #33 |
| #40 | Dashboard (config, monitoring, reports) | Deferred, depends on #33, #55 |

This milestone introduces:
- JSONL-backed run history (run, phase-run, and file-change records;
  stdlib only — the original SQLite design conflicted with the
  zero-dependency and CGO_ENABLED=0 invariants)
- Context7 MCP server for library documentation lookup
- Post-merge CI monitoring with automatic revert on failure
- Web dashboard with React frontend embedded via embed.FS

### Security track

| Issue | Description |
|-------|-------------|
| #71 | Quarantine untrusted input with Dual LLM pattern (symbolic `$VAR` references; quarantine model placement is a per-repo choice) |

### Multi-App Sandbox Architecture (#41)

Tracking issue for the full v2 migration. All milestones above roll up to
this umbrella issue.

---

## Codebase Stats

- **Language:** Go 1.26, stdlib only (zero external dependencies)
- **Module:** `github.com/ruromero/la-fabriquilla`
- **Source lines:** ~8,600 lines of Go
- **Binaries:** 10 (dispatcher + 8 phase binaries + eval)
- **Test coverage:** Unit tests with `-race` flag
- **License:** Apache 2.0

---

## Key Interfaces

```go
// Token retrieval (static token or GitHub App JWT exchange)
type TokenSource interface {
    Token(ctx context.Context) (string, error)
}

// Tool execution (MCP client, context tools, composite router)
type ToolHandler interface {
    Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

// Pipeline state persistence
type StateStore interface {
    Save(ctx context.Context, key string, state *State) error
    Load(ctx context.Context, key string) (*State, error)
    Delete(ctx context.Context, key string) error
}
```

---

## Conventions

From `CONVENTIONS.md`:

- **Go:** stdlib only, `gofmt`, `go vet`, structured logging via `slog`
- **Packages:** single-word names, descriptive exported types
- **Architecture:** direct HTTP calls, no LLM frameworks, all judgment deferred to LLMs
- **Security:** no credentials in prompts, CODEOWNERS/CONVENTIONS never agent-modifiable, different model family for review vs generation
- **Config:** fail-fast validation at startup
- **Sandbox:** deterministic config, bounded timeouts (never `context.WithoutCancel`)
- **Testing:** unit tests with race detector
