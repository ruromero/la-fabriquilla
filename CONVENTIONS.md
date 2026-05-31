# Conventions

## Go

- Zero external dependencies — stdlib only
- `gofmt` formatting enforced in CI
- `go vet` must pass with no warnings
- Tests use `testing` package, no external test frameworks
- Errors wrap with `fmt.Errorf("context: %w", err)`
- Use `log/slog` for structured logging, never `log` or `fmt.Println`
- Use `context.Context` as first parameter on all I/O functions

## Naming

- Packages are single lowercase words
- Exported types use descriptive nouns: `Client`, `ChatRequest`, `PlanResult`
- Constructors are `NewX` functions
- Interface methods match the verb they perform: `Execute`, `Chat`

## Architecture

- No LLM frameworks — direct HTTP calls to Ollama and Gemini APIs
- All judgment deferred to LLMs via prompts, no judgment in Go code
- Agent prompts are string constants in their respective files
- Config is JSON file, not env vars
- All untrusted input must pass through `sandbox.SanitizeInput`

## Config

- Config structs with `Enabled` flags must validate dependent fields
  in `LoadConfig` — fail fast at startup, not at runtime
- Network endpoints in deploy configs must match the actual deployment
  topology (k8s service DNS, not localhost)

## Sandbox / Container execution

- Upload ALL files a sandboxed binary needs, not just the primary
  state file — check every env var that points to a file path
- Cleanup operations (sandbox destroy, temp file removal) must use a
  bounded timeout (`context.WithTimeout(context.Background(), ...)`)
  — never an unbounded context
- Deterministic config (sandbox name, policy path) must be computed
  once before retry loops, not rebuilt on each attempt

## Security

- Credentials never appear in prompts, logs, or agent context
- No agent output may modify agent configuration or prompts
- Review phase must use a different model family than code generation
- CODEOWNERS, CONVENTIONS.md, CLAUDE.md are human-owned — never agent-modifiable

## Testing

- Unit tests for all packages with non-trivial logic
- Test files named `*_test.go` in the same package
- Use `t.Run` for subtests
- Race detector enabled in CI (`go test -race`)
