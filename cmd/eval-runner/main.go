// Command eval-runner executes golden-set eval cases against real
// inference endpoints and appends results to a JSONL history.
//
// Usage:
//
//	eval-runner -config config.json [-phase planner,coder] [-case substr]
//	            [-model NAME] [-runs N] [-compare]
//	            [-models NAME,NAME,...] (compare multiple models side-by-side)
//
// When -models is set, each model is used as both the agent model (for
// planner/designer/coder cases) and the arbiter model (for arbiter cases).
// The inference URL and arbiter URL remain fixed from config. To compare
// only non-arbiter phases use -phase planner,designer,coder; to compare
// arbiter models use -phase arbiter.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/eval"
	"github.com/ruromero/la-fabriquilla/inference"
)

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config.json")
	casesDir := flag.String("cases", "", "cases dir (default from config)")
	phaseFilter := flag.String("phase", "", "comma-separated phase filter")
	caseFilter := flag.String("case", "", "substring filter on case name")
	model := flag.String("model", "", "agent model override")
	modelsFlag := flag.String("models", "", "comma-separated models to compare side-by-side (use -runs 10+ for statistical reliability)")
	arbBaseURL := flag.String("arbiter-base-url", "", "arbiter endpoint override")
	arbModel := flag.String("arbiter-model", "", "arbiter model override")
	runs := flag.Int("runs", -1, "runs per case (-1 = config, 0 = threshold denominator)")
	timeout := flag.Duration("timeout", 0, "per-run timeout (default from config)")
	resultsDir := flag.String("results", "", "results dir (default from config)")
	benchmarkDir := flag.String("benchmark-dir", "docs/benchmarks", "directory to write model comparison reports (used with -models)")
	noBenchmark := flag.Bool("no-benchmark", false, "skip writing benchmark report to -benchmark-dir (useful for quick smoke tests)")
	compare := flag.Bool("compare", false, "print comparison table from results history and exit")
	flag.Parse()

	if *benchmarkDir != "" {
		abs, err := filepath.Abs(*benchmarkDir)
		if err != nil {
			slog.Error("resolve benchmark dir", "error", err)
			os.Exit(1)
		}
		*benchmarkDir = abs
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	dir := cfg.Eval.ResultsDir
	if *resultsDir != "" {
		dir = *resultsDir
	}

	if *compare {
		records, err := eval.LoadRecords(dir)
		if err != nil {
			slog.Error("load records", "error", err)
			os.Exit(1)
		}
		fmt.Print(eval.FormatComparison(records))
		return
	}

	if cfg.Inference.BaseURL == "" {
		slog.Error("inference.base_url is required in config")
		os.Exit(1)
	}

	cdir := cfg.Eval.CasesDir
	if *casesDir != "" {
		cdir = *casesDir
	}
	allCases, err := eval.LoadTestCases(cdir)
	if err != nil {
		slog.Error("load cases", "dir", cdir, "error", err)
		os.Exit(1)
	}
	cases := filterCases(allCases, *phaseFilter, *caseFilter)
	if len(cases) == 0 {
		slog.Warn("no cases matched", "dir", cdir, "phase", *phaseFilter, "case", *caseFilter)
		return
	}

	sha := gitSHA()

	if *modelsFlag != "" {
		modelList, err := splitModels(*modelsFlag)
		if err != nil {
			slog.Error("invalid -models flag", "error", err)
			os.Exit(1)
		}
		if len(modelList) == 0 {
			slog.Error("no models parsed from -models flag")
			os.Exit(1)
		}

		matrixResults := make(map[string][]eval.RunResult, len(modelList))
		var matrixSkipped []string

		for _, m := range modelList {
			if err := preflightOllama(cfg.Inference.BaseURL, m); err != nil {
				slog.Error("preflight failed", "model", m, "error", err)
				os.Exit(1)
			}

			a := &adapters{
				agentClient: inference.NewClient(cfg.Inference.BaseURL, inference.WithAPIKey(cfg.Inference.APIKey)),
				agentModel:  m,
				timeout:     cfg.Eval.TimeoutPerRun.Duration,
			}
			if *timeout > 0 {
				a.timeout = *timeout
			}
			arbURL := cfg.Arbiter.BaseURL
			if *arbBaseURL != "" {
				arbURL = *arbBaseURL
			}
			if arbURL != "" {
				a.arbClient = inference.NewClient(arbURL, inference.WithAPIKey(cfg.Arbiter.APIKey))
				a.arbModel = m
			}

			results, skipped, err := runCases(a, cases, *runs, cfg, dir, sha)
			if err != nil {
				slog.Error("run cases", "model", m, "error", err)
				os.Exit(1)
			}
			matrixResults[m] = results
			if len(matrixSkipped) == 0 {
				matrixSkipped = skipped
			}
		}

		anyRan := false
		for _, modelRuns := range matrixResults {
			if len(modelRuns) > 0 {
				anyRan = true
				break
			}
		}
		if !anyRan && len(matrixSkipped) > 0 {
			slog.Error("all matched cases were skipped; check -phase filter and arbiter config", "skipped", matrixSkipped)
			os.Exit(1)
		}

		report := eval.FormatModelMatrix(modelList, matrixResults, matrixSkipped)
		fmt.Print(report)

		if *benchmarkDir != "" && !*noBenchmark {
			path, err := eval.WriteBenchmark(*benchmarkDir, *phaseFilter, sha, modelList, matrixResults, report)
			if err != nil {
				slog.Warn("write benchmark file", "error", err)
			} else {
				slog.Info("benchmark written", "path", path)
			}
		}

		for _, modelRuns := range matrixResults {
			for _, r := range modelRuns {
				if !r.Pass {
					os.Exit(1)
				}
			}
		}
		return
	}

	// Single-model mode.
	agentModel := cfg.Inference.Model
	if *model != "" {
		agentModel = *model
	}
	if agentModel == "" {
		slog.Error("agent model required: set inference.model in config or pass -model")
		os.Exit(1)
	}
	if err := preflightOllama(cfg.Inference.BaseURL, agentModel); err != nil {
		slog.Error("preflight failed", "error", err)
		os.Exit(1)
	}

	a := &adapters{
		agentClient: inference.NewClient(cfg.Inference.BaseURL, inference.WithAPIKey(cfg.Inference.APIKey)),
		agentModel:  agentModel,
		timeout:     cfg.Eval.TimeoutPerRun.Duration,
	}
	if *timeout > 0 {
		a.timeout = *timeout
	}

	arbURL, arbMdl := cfg.Arbiter.BaseURL, cfg.Arbiter.Model
	if *arbBaseURL != "" {
		arbURL = *arbBaseURL
	}
	if *arbModel != "" {
		arbMdl = *arbModel
	}
	if arbURL != "" && arbMdl != "" {
		a.arbClient = inference.NewClient(arbURL, inference.WithAPIKey(cfg.Arbiter.APIKey))
		a.arbModel = arbMdl
	} else {
		slog.Warn("arbiter endpoint not configured — arbiter cases will be skipped")
	}

	results, skipped, err := runCases(a, cases, *runs, cfg, dir, sha)
	if err != nil {
		slog.Error("run cases", "error", err)
		os.Exit(1)
	}

	if len(results) == 0 && len(skipped) > 0 {
		slog.Error("all matched cases were skipped; check -phase filter and arbiter config", "skipped", skipped)
		os.Exit(1)
	}

	fmt.Print(eval.FormatReport(results, skipped))

	for _, r := range results {
		if !r.Pass {
			os.Exit(1)
		}
	}
}

// runCases executes cases against the given adapters, appending each result
// to the JSONL history. runsOverride < 0 uses cfg.Eval.RunsPerCase;
// runsOverride == 0 uses the threshold denominator.
func runCases(a *adapters, cases []eval.TestCase, runsOverride int, cfg config.Config, dir, sha string) ([]eval.RunResult, []string, error) {
	var results []eval.RunResult
	var skipped []string
	for _, tc := range cases {
		fn, ok := a.outputFunc(tc.Phase)
		if !ok {
			skipped = append(skipped, tc.Phase+"/"+tc.Name)
			continue
		}
		caseRuns := runsOverride
		if caseRuns < 0 {
			caseRuns = cfg.Eval.RunsPerCase
		}
		if caseRuns == 0 {
			_, total, err := eval.ParseThreshold(tc.PassThreshold)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid threshold for %s: %w", tc.Name, err)
			}
			caseRuns = total
		}

		runModel := a.agentModel
		if tc.Phase == "arbiter" {
			runModel = a.arbModel
		}
		slog.Info("running case", "case", tc.Phase+"/"+tc.Name, "runs", caseRuns, "model", runModel)
		start := time.Now()
		result, err := eval.RunCaseE(tc, caseRuns, fn)
		if err != nil {
			return nil, nil, fmt.Errorf("run case %s: %w", tc.Name, err)
		}
		wall := time.Since(start)
		prompt, comp := a.takeUsage()
		results = append(results, result)

		rec := eval.Record{
			Timestamp:    time.Now().UTC(),
			GitSHA:       sha,
			Case:         result.Case,
			Phase:        tc.Phase,
			Model:        runModel,
			ArbiterModel: a.arbModel,
			Runs:         result.Runs,
			Passes:       result.Passes,
			Threshold:    result.Threshold,
			Pass:         result.Pass,
			PromptTokens: prompt,
			CompTokens:   comp,
			WallTimeSecs: wall.Seconds(),
			Failures:     result.Failures,
		}
		if err := eval.AppendRecord(dir, rec); err != nil {
			return nil, nil, fmt.Errorf("append record: %w", err)
		}
	}
	return results, skipped, nil
}

// filterCases applies a comma-separated phase filter and a case-name
// substring filter. Empty filters match everything.
func filterCases(cases []eval.TestCase, phases, nameSubstr string) []eval.TestCase {
	allowed := map[string]bool{}
	for _, p := range strings.Split(phases, ",") {
		if p = strings.TrimSpace(p); p != "" {
			allowed[p] = true
		}
	}
	var out []eval.TestCase
	for _, tc := range cases {
		if len(allowed) > 0 && !allowed[tc.Phase] {
			continue
		}
		if nameSubstr != "" && !strings.Contains(tc.Name, nameSubstr) {
			continue
		}
		out = append(out, tc)
	}
	return out
}

func splitModels(s string) ([]string, error) {
	seen := make(map[string]bool)
	var out []string
	for _, m := range strings.Split(s, ",") {
		if m = strings.TrimSpace(m); m != "" {
			if seen[m] {
				return nil, fmt.Errorf("duplicate model %q in -models flag", m)
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out, nil
}

func defaultConfigPath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "config.json"
}

func gitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
