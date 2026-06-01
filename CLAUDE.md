# CLAUDE.md

## Project

Autonomous software development orchestrator. Polls GitHub issues,
drives a phased pipeline (research → plan → design → code → review →
iterate) using local LLMs via Ollama and Gemini for research.

## Stack

- Language: Go 1.26+
- Inference: Ollama API (local models) + Gemini API (research)
- MCP: Serena (LSP tools), Context7 (library docs)
- Deploy: k8s (k3s single-node, no GPU required for this pod)
- Container registry: Quay.io

## Build & Test

```bash
gofmt -l .
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build ./...
```

## Constraints

- Zero external Go dependencies — stdlib only (net/http, encoding/json)
- No LLM frameworks (no LangChain, no CrewAI)
- The orchestrator handles mechanics only — all judgment is deferred
  to LLMs via prompts (zero framework cognition principle)
- Config is a JSON file, not env vars (supports multi-repo)
- Inference uses OpenAI-compatible API (works with Ollama, DeepSeek, Gemini, etc.)
- Models support function calling for MCP tool integration
- Single GPU (RTX 3060 12GB) shared across all repos — only one
  inference runs at a time

## Structure

- cmd/dispatcher/ — poll loop, phase orchestration
- cmd/{gatherer,researcher,planner,designer,coder,committer}/ — phase binaries
- cmd/internal/ — shared helpers (config/state loading, GitHub client)
- config/ — shared configuration types
- github/ — GitHub API client (issues, PRs, comments, labels)
- inference/ — OpenAI-compatible inference client (chat + tool-calling loop)
- gemini/ — Gemini API client (research phase)
- mcp/ — MCP client (JSON-RPC over stdio, tool discovery)
- sandbox/ — input sanitization (Unicode normalization)
- harness/ — phase context assembly
- traces/ — structured JSON trace logging
- agents/ — agent phases (planner, designer, coder, reviewer, iterator)

## Security invariants

- Credentials never enter agent prompts or LLM context
- Credentials and tool argument values never appear in log output
- No agent output can modify agent configuration or prompts
- All untrusted input is sanitized before entering agent context
- Review must use a different model family than code generation
