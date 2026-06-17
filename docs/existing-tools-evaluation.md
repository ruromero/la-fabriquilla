# Should La Fabriquilla Adopt an Existing AI Coding Tool?

An evaluation of whether Claude Code, OpenCode, Aider, SWE-agent,
OpenHands, Cline, Continue, or Cursor could replace or augment our
custom orchestrator.

---

## What We Built and Why

La Fabriquilla is a purpose-built autonomous orchestrator. It polls GitHub
issues, runs them through a phased pipeline (gather → research → plan →
design → code → review → iterate), and opens PRs. Each phase uses a
different model suited to its task. The system enforces sandbox isolation
per phase, scoped GitHub credentials per trust boundary, and deterministic
guardrails in Go code.

The architecture is shaped by constraints that are choices, not
limitations:

- **Zero external Go dependencies** — stdlib only
- **Zero framework cognition** — the orchestrator handles mechanics; all
  judgment is deferred to LLMs via prompts
- **Local inference first** — Ollama on a single consumer GPU, with
  external APIs only for phases that need them
- **Security as foundation** — sandboxed phases, scoped credentials,
  blocked self-modification paths

These constraints define the tool, not the other way around. Any candidate
replacement must satisfy all of them or offer something compelling enough
to justify relaxing them.

---

## The Candidates

### Claude Code

Anthropic's CLI agent. Rich ecosystem of skills, plugins, MCP servers, and
a subagent orchestration model.

**Why it looks appealing:** First-class MCP support, mature plugin
ecosystem, headless CI mode (`claude -p`), Agent SDK for programmatic
control, multi-agent workflows via subagents.

**Why it doesn't fit:**

- **Vendor-locked to Anthropic models.** Cannot use Ollama, local models,
  or any other provider. Every inference call goes through the Anthropic
  API. This breaks our local-first constraint and introduces per-token
  costs that scale with issue volume.
- **No Go SDK.** The Agent SDK exists in Python and TypeScript. Integrating
  it into our Go orchestrator means either rewriting in another language or
  shelling out to a Claude Code subprocess — adding a dependency on
  Anthropic's CLI binary, its Node.js runtime, and its release cadence.
- **Permission model, not sandbox isolation.** Claude Code uses an ML
  classifier and approval gates. It does not run phases in Docker
  containers with network policies. A prompt-injected model can still
  access the filesystem. Our threat model requires physical network
  isolation per phase.
- **No phased pipeline abstraction.** You can compose multi-step workflows
  with subagents, but there is no concept of phase-specific model
  assignment, inter-phase state serialization, or per-phase credential
  scoping. We would have to rebuild all of that on top of Claude Code's
  primitives.

**Verdict:** Wrong trust model, wrong cost model, wrong language. The
plugin ecosystem is impressive but solves problems we don't have (IDE
integration, interactive pair programming).

---

### OpenCode

Go-based CLI coding agent with TUI. MIT licensed. Supports 75+ providers
including Ollama.

**Why it looks appealing:** Written in Go. MIT licensed. Supports Ollama
natively. Has a headless mode for CI. Docker sandbox support. MCP
integration. Closest in stack and spirit to what we're building.

**Why it doesn't fit:**

- **Not a library.** OpenCode is a standalone CLI/TUI application. It
  cannot be imported as a Go package into our orchestrator. Using it means
  shelling out to `opencode` as a subprocess, which defeats the purpose of
  having a Go codebase.
- **Single-task agent.** It has two built-in agents (build and plan) but no
  concept of a multi-phase pipeline. Each invocation processes one prompt
  in one conversation. Our pipeline needs seven phases with different
  models, different tool sets, different network policies, and cumulative
  state passed between them.
- **No per-phase credential scoping.** OpenCode authenticates once and
  gives the agent full access. We need the gatherer to have read-only
  GitHub access, the coder to have zero GitHub access, and the committer to
  be the only identity that can push.
- **Would add an external dependency.** Even if we could use it, importing
  OpenCode brings its dependency tree into our project. Our constraint is
  zero external Go dependencies.

**Verdict:** If we were starting from scratch and building a simpler
single-task agent, OpenCode would be a reasonable foundation. But it
doesn't scale to our phased pipeline model and violates our dependency
constraint.

---

### Aider

Terminal-first, Git-native AI pair programmer. Apache 2.0. Ollama support.

**Why it looks appealing:** Strong Git integration (every change is an
atomic commit), wide model support, well-tested on coding benchmarks.

**Why it doesn't fit:**

- **Interactive pair programmer, not autonomous agent.** Aider is designed
  for a human sitting at a terminal making decisions. `--yes-always`
  exists but it's a bolt-on, not a first-class headless mode.
- **No MCP support.** Open RFC but not shipped. We rely on Serena MCP for
  LSP-powered code navigation across phases.
- **No sandboxing.** Runs directly in your repo with full filesystem and
  network access.
- **No multi-phase pipeline.** Single-threaded, single-conversation.
- **Python.** Not embeddable in Go.

**Verdict:** Excellent at what it does (interactive pair programming) but
architecturally irrelevant to autonomous pipeline orchestration.

---

### SWE-agent (Princeton)

Academic research project for autonomous software engineering. Takes a
GitHub issue and produces a patch.

**Why it looks appealing:** Designed for exactly our trigger model (GitHub
issue → code change). MIT licensed. Docker sandboxing. Supports Ollama via
LiteLLM. mini-SWE-agent (100 lines of Python) achieves strong results on
SWE-bench.

**Why it doesn't fit:**

- **Single-phase.** SWE-agent's loop is: search → edit → test → iterate.
  There is no separate research phase, no design phase, no review by a
  different model. All judgment comes from one model in one conversation.
- **No multi-model assignment.** We use different models for different
  phases because the tasks have different characteristics (research needs
  breadth, review needs judgment, coding needs tool-calling). SWE-agent
  uses one model for everything.
- **Research tool, not production infrastructure.** No credential
  management, no GitHub App auth, no state persistence, no guardrails.
- **Python.** Not embeddable in Go.

**Why it's still interesting:** mini-SWE-agent's result validates our
design philosophy. If 100 lines of scaffolding can achieve strong
benchmark results, then framework complexity is not where the value is. The
value is in model quality and prompt design — which is exactly our "zero
framework cognition" principle. The scaffolding should be simple and
mechanical; the intelligence lives in the prompts.

**Verdict:** Validates our approach. Not a replacement.

---

### OpenHands (formerly OpenDevin)

Open-source autonomous software engineering platform. MIT licensed. $18.8M
Series A. Docker sandboxing, GitHub issue triggers, composable Python SDK.

**Why it looks appealing:** This is the strongest candidate. It supports
Ollama, runs headless, has Docker sandboxing, can be triggered from GitHub
issues (label `openhands` or mention `@openhands`), and has a composable
SDK for defining custom agents. It even supports multi-agent parallel
execution.

**Why it still doesn't fit:**

- **Python SDK.** The composable agent definitions are Python. Our
  orchestrator is Go. Integrating means either rewriting in Python (losing
  all our Go code) or calling OpenHands as a subprocess (adding a Python
  runtime dependency and a complex distributed system boundary).
- **Different abstraction level.** OpenHands' CodeAct agent handles
  planning, coding, and testing in a single agent loop. Our architecture
  deliberately separates these into distinct phases with different models,
  different sandboxes, and different credential scopes. Mapping our phased
  pipeline onto OpenHands means fighting its agent model.
- **Event-driven architecture vs. sequential phases.** OpenHands uses
  immutable events for all actions/observations. Our pipeline is simpler:
  each phase reads state, produces output, writes state. The event-driven
  model adds complexity we don't need.
- **Dependency weight.** OpenHands brings LiteLLM, Docker SDK, its event
  system, and dozens of Python dependencies. We have zero external
  dependencies.
- **Security model mismatch.** OpenHands sandboxes the entire agent in one
  Docker container. We sandbox each phase separately with different network
  policies. A coder sandbox that can reach Ollama but not GitHub is
  fundamentally different from a single sandbox that runs the whole
  pipeline.

**Where it could augment:** If we ever wanted to replace a single phase
(e.g., the coder phase) with a more capable agent, OpenHands' CodeAct
could run inside our sandbox as the execution engine for that phase. But
this would mean our coder phase shells out to a Python process running
OpenHands — a significant complexity increase for uncertain benefit.

**Verdict:** Most architecturally relevant candidate. Still wrong language,
wrong dependency model, and wrong sandboxing granularity. Worth watching as
the field evolves.

---

### Cline

Open-source autonomous AI agent. Apache 2.0. VS Code extension with CLI
(2.0). Rich MCP marketplace.

**Why it doesn't fit:** IDE-centric design. The CLI exists but is not a
pipeline engine. No Docker sandboxing — relies on human approval gates.
Not embeddable as a library. TypeScript. The MCP marketplace is impressive
but we already have MCP integration (Serena, Context7) via our own Go
client.

**Verdict:** Wrong architecture (IDE extension), wrong trust model
(approval gates instead of sandboxes).

---

### Continue

Open-source AI code assistant. Apache 2.0. VS Code/JetBrains extension
with CLI.

**Why it doesn't fit:** Assistant-oriented, not an autonomous pipeline
runner. No sandboxing. Not embeddable in Go. TypeScript/Node.js core.
Primarily designed for autocomplete, chat, and interactive editing.

**Verdict:** Different category of tool entirely (assistant vs.
orchestrator).

---

### Cursor

AI-first code editor. Proprietary. Cloud-dependent.

**Why it doesn't fit:** Proprietary, cloud-only inference, no local model
support, $20-200/month per seat. The Background Agent and SDK are
interesting but completely vendor-locked. No self-hosting.

**Verdict:** Wrong on every constraint. Proprietary, cloud-dependent, no
local models, not embeddable.

---

## Synthesis

### Why none of these tools replace our orchestrator

The tools above solve a different problem. They are **single-task coding
agents** — give them a prompt (or an issue), and they produce code. Some
do it interactively (Aider, Cline, Continue, Cursor), some do it
autonomously (SWE-agent, OpenHands), and some do both (Claude Code,
OpenCode).

Our orchestrator is not a coding agent. It is a **pipeline that
orchestrates multiple agents**, each with:

- A different model (local Ollama vs. Gemini vs. DeepSeek)
- A different tool set (doc tools vs. Serena read-only vs. Serena
  read-write)
- A different sandbox (different network policy per phase)
- Different credentials (read-only vs. no-GitHub vs. write-only)
- A specific role in a sequence (research informs planning, planning
  informs design, design informs coding, a different model reviews the
  code)

No existing tool provides this combination. The closest (OpenHands) gets
multi-agent execution right but packages it in a Python SDK with a
fundamentally different sandboxing model.

### What we'd lose by adopting any of them

1. **Per-phase credential scoping.** Every tool gives its agent a single
   identity. We give each phase the minimum permissions it needs.
2. **Per-phase network isolation.** Every tool either sandboxes everything
   in one container or doesn't sandbox at all. We isolate each phase
   independently.
3. **Zero-dependency Go binary.** Every tool introduces a runtime
   dependency (Node.js, Python, Docker SDK libraries). We ship a single
   static binary.
4. **Model diversity per phase.** Every tool uses one model per
   conversation. We assign models per phase based on task characteristics.
5. **Deterministic guardrails.** Our guardrails (iteration limits, cost
   budgets, blocked paths, scope limits) are in Go code, not in prompts.
   Adopting another tool means either losing them or reimplementing them as
   wrappers around the tool.

### What we can learn from them

1. **mini-SWE-agent proves scaffolding simplicity works.** 100 lines
   achieving strong benchmark results validates that framework complexity
   is not the bottleneck. Keep our orchestrator simple and mechanical.
2. **MCP is the right integration surface.** Claude Code, OpenCode, and
   Cline all converge on MCP for tool integration. Our existing MCP client
   (Serena, Context7) aligns with the industry direction.
3. **OpenHands' event-driven replay is worth studying.** Immutable events
   enabling pause/resume and deterministic replay is elegant. If we ever
   need that capability, their architecture is the reference implementation.
4. **Cline's MCP marketplace shows the ecosystem direction.** The density
   of available MCP servers means we can add capabilities (databases, CI/CD
   systems, monitoring) by configuring new MCP servers rather than writing
   new code.

### Could any of them augment a single phase?

In theory, we could replace the coder phase with a more capable autonomous
agent (OpenHands, SWE-agent) running inside our sandbox. The dispatcher
would upload state into the sandbox, run the external agent, and download
the result.

In practice, this means:

- Adding Python to our sandbox images
- Debugging two agent loops (ours + the external tool's)
- Losing control over the coder's tool-calling behavior
- Introducing a dependency on the external tool's release cadence
- Getting uncertain benefit — our coder phase works, and model quality
  improvements will lift all tools equally

This might make sense if a specific phase consistently underperforms and a
specialized tool demonstrably does better. We'd want to measure that with
our golden-set evaluation harness before committing.

---

## Conclusion

Build, don't adopt. Our constraints are load-bearing — they exist because
the system is an autonomous agent with GitHub write access running
unattended. Security boundaries, credential scoping, and deterministic
guardrails are not features to be added later; they are the architecture.
No existing tool provides them at the granularity we need.

The value of these tools is in validating our design decisions: simple
scaffolding works, MCP is the right integration protocol, and per-model
phase assignment is architecturally rare — which means it's our
differentiator, not something to give up.

Keep building. Keep watching. Revisit when a tool offers embeddable,
language-agnostic phase execution with per-phase sandboxing and credential
scoping.
