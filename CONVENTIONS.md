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

## Slice & map safety

- Never filter a slice in-place with `s[:0]` — this reuses the backing
  array and a subsequent append can silently overwrite elements past the
  new length. Always allocate a fresh `var result []T`.
- When checking "expected set vs actual set," use a separate `seen` map
  instead of `delete`-ing from the expected map. Mutating the map you
  are iterating against hides mismatches and makes the success condition
  depend on deletion order.

## Test doubles

- When a stub records operations for assertions (created PRs, posted
  comments), the recorded struct must capture all caller-supplied
  arguments — not just the return value fields. A field the caller
  passes (e.g. PR body) but the stub discards cannot be verified later.

## Testing

- Unit tests for all packages with non-trivial logic
- Test files named `*_test.go` in the same package
- Use `t.Run` for subtests
- Race detector enabled in CI (`go test -race`)

### Test quality

- Tests must call real production functions — never reimplement logic
  locally to approximate what the real code does
- Use production types directly (e.g. `review.ArbiterResult`, not a
  local struct with the same JSON tags)
- Test inputs must include the data shapes production code actually
  produces — if production schemas use `[]string`, test with `[]string`
  not just `[]any`
- Every test must pass its input through a real code path and assert an
  observable outcome — `_ = payload` followed by a static check is not
  a test
- When the function under test lives in a package you can't import
  (circular dependency), move the test to that package rather than
  building a local approximation
- Use stdlib helpers (`strings.Contains`, `strings.HasPrefix`) — don't
  write custom string search functions in test files

### Security tests

- Adversarial corpus lives in `testdata/adversarial/*.json`, each file
  is `[]adversarialCase` with fields `name`, `category`, `input`
- Load via `loadAdversarialCorpus(t)` helper (returns
  `map[string][]adversarialCase`)
- Iterate with nested `t.Run(category, func() { t.Run(tc.Name, ...) })`
- Security test files are named `*_security_test.go` or
  `*_injection_test.go`
- HTTP boundary tests use `httptest.NewServer` to capture and inspect
  requests sent by production code

### General test patterns

- Table-driven: `for _, tt := range tests { t.Run(tt.name, ...) }`
- Mocks implement production interfaces with minimal fields — never
  duplicate production structs
- Tests stay in the same package as production code (no `_test` suffix
  packages)
