# Model Selection Rationale

## Evaluation methodology

Each model is evaluated against golden test cases graded by difficulty
(easy/medium/hard). Cases use two assertion layers:

1. **Deterministic checks** — string matching, outcome verification (free, fast)
2. **LLM-as-judge** — Gemini 2.5 Pro evaluates agent output against
   directive-based criteria (semantic, near-deterministic)

The judge is always from a different model family than the model under test
to avoid self-preference bias.

Each case runs 10 times to measure consistency. A model that scores 7/10 on
hard cases is more useful than one that scores 10/10 on easy cases.

## Models evaluated

| Model | Size | Type | Quantization |
|-------|------|------|-------------|
| qwen2.5-coder:14b | 9.0 GB | Code-specialized | Q4_K_M |
| gemma4:12b | 7.6 GB | General-purpose | Q4_K_M |
| qwen3.5:35b-a3b | 23.9 GB | MoE general-purpose | Q4_K_M |

All models run locally on a single RTX 3060 12 GB via Ollama.

## Results

### Planner phase

Tests whether the model can decompose issues into actionable plans,
detect hidden complexity (goroutine safety, cross-repo concerns), and
know when to ask for more information.

| Case (Difficulty) | qwen2.5-coder:14b | gemma4:12b | qwen3.5:35b-a3b |
|-------------------|--------------------|------------|------------------|
| simple-bug (easy) | 10/10 (177s) | 10/10 (189s) | 10/10 |
| ambiguous-issue (easy) | 10/10 (18s) | 10/10 (81s) | 10/10 |
| complex-task (medium) | 10/10 (195s) | 10/10 (413s) | 10/10 |
| hidden-complexity (hard) | **0/10** (224s) | **10/10** (405s) | **10/10** (1252s) |

gemma4:12b initially scored 1/10 on simple-bug — it proposed guard
clauses without specifying the error-handling action. A single prompt
directive ("state the action on failure") fixed this to 10/10,
demonstrating the gap was prompt-addressable, not a model limitation.

qwen2.5-coder passes easy/medium cases through pattern matching but
completely fails the hard case — it never identifies goroutine safety
requirements or cross-repo budget sharing concerns.

### Designer phase

Tests whether the model can produce structured designs with specific file
paths, function signatures, and correct use of sync primitives.

| Case (Difficulty) | qwen2.5-coder:14b | gemma4:12b | qwen3.5:35b-a3b |
|-------------------|--------------------|------------|------------------|
| structured-design (easy) | 10/10 (185s) | 10/10 (522s) | 10/10 |
| stdlib-constraint (easy) | 9/10 (308s) | 9/10 (555s) | 10/10 |
| handles-concurrency (medium) | **0/10** (301s) | **10/10** (532s) | **9/10** (1103s) |

qwen2.5-coder mentions sync vaguely but never specifies which primitive
or how to use it. gemma4 is perfectly consistent; qwen3.5 drops 1 run
in 10.

### Coder phase

Tests whether the model can generate correct, compilable Go code that
handles errors properly and respects project conventions.

| Case (Difficulty) | qwen2.5-coder:14b |
|-------------------|--------------------|
| adds-new-function (easy) | 10/10 (72s) |
| modifies-existing (easy) | 10/10 (72s) |
| resists-injection (easy) | 10/10 (70s) |
| error-handling (hard) | **10/10** (333s) |

qwen2.5-coder excels at code generation, scoring 10/10 even on the hard
case that requires validation-before-return, distinct error messages,
and no global state.

## Selected configuration

| Role | Model | Rationale |
|------|-------|-----------|
| **Planner** | gemma4:12b | 10/10 on all cases including hard; 3x faster than qwen3.5 |
| **Designer** | gemma4:12b | 10/10 on concurrency case; consistent and efficient |
| **Coder** | qwen2.5-coder:14b | Purpose-built for code; 10/10 on hard error-handling case |
| **Reviewer** | deepseek-chat (API) | Different model family from coder; API-based for independence |

## Models considered but rejected

### qwen2.5-coder:14b for planner/designer

Code-specialized models optimize for syntax and patterns but lack the
systems-level reasoning needed for planning and design. qwen2.5-coder
generates correct Go code but scores 0/10 on planning tasks that require
reasoning about goroutine safety, concurrent access patterns, or
cross-component coordination. This is a fundamental model limitation,
not addressable via prompt tuning.

### qwen3.5:35b-a3b (MoE, 35B params, 3B active)

Strong quality results (10/10 planner, 9/10 designer) but impractical
on a single RTX 3060 12 GB:

- **3x slower than gemma4**: 1252s vs 405s per planner case, 1103s vs
  532s per designer case
- **Higher VRAM pressure**: 23.9 GB model requires heavy CPU offloading,
  causing the speed gap
- **Marginal quality gain**: Matches gemma4 on planning, slightly worse
  on design (9/10 vs 10/10)
- **GPU contention**: On a single-GPU setup shared across repos, the
  slower model blocks the pipeline for other work

If the hardware constraint changes (multi-GPU or API-hosted), qwen3.5
would be worth re-evaluating.

### qwen14b-opencode

Not benchmarked with llm_judge assertions. Earlier shallow-assertion
benchmarks showed no advantage over qwen2.5-coder:14b for coding tasks.
The model would need to demonstrate clear superiority on the hard cases
to justify inclusion.

### gemma4:12b for coder

Not benchmarked for code generation. gemma4 is a general-purpose model
without code-specific training. Given qwen2.5-coder's 10/10 on all
coder cases including the hard one, there is no motivation to test
gemma4 as a coder unless qwen2.5-coder regresses.

### Frontier models (Gemini Pro, Claude Opus, DeepSeek)

Not yet benchmarked against the same cases. The eval framework supports
API-hosted models via the endpoint registry (`model@endpoint` syntax).
A cost/quality comparison against frontier models would answer whether
the quality gain justifies the per-token cost for each phase. This is
planned as a follow-up evaluation.

## Prompt tuning findings

| Model | Case | Before | After | Fix |
|-------|------|--------|-------|-----|
| gemma4:12b | simple-bug | 1/10 | 10/10 | Added "state the action on failure" directive to planner system prompt |

This demonstrates that eval-driven prompt tuning is effective for
closing specific behavioral gaps. The eval framework makes it possible
to identify the exact criterion a model fails on, craft a targeted
prompt fix, and verify the fix with statistical confidence.

## Reproducing these results

```bash
# Planner baseline (all models, 10 runs, Gemini judge)
./bin/eval-runner -config config.json -phase planner \
  -models qwen2.5-coder:14b,gemma4:12b,qwen3.5:35b-a3b \
  -runs 10 -judge-model gemini-2.5-pro@gemini

# Designer baseline
./bin/eval-runner -config config.json -phase designer \
  -models qwen2.5-coder:14b,gemma4:12b,qwen3.5:35b-a3b \
  -runs 10 -judge-model gemini-2.5-pro@gemini

# Coder baseline
./bin/eval-runner -config config.json -phase coder \
  -model qwen2.5-coder:14b \
  -runs 10 -judge-model gemini-2.5-pro@gemini
```
