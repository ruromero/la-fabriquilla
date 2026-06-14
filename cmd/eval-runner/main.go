// Command eval-runner executes golden-set eval cases against real
// inference endpoints and appends results to a JSONL history.
//
// Usage:
//
//	eval-runner -config config.json [-phase planner,coder] [-case substr]
//	            [-model NAME] [-runs N] [-compare]
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	arbBaseURL := flag.String("arbiter-base-url", "", "arbiter endpoint override")
	arbModel := flag.String("arbiter-model", "", "arbiter model override")
	runs := flag.Int("runs", -1, "runs per case (-1 = config, 0 = threshold denominator)")
	timeout := flag.Duration("timeout", 0, "per-run timeout (default from config)")
	resultsDir := flag.String("results", "", "results dir (default from config)")
	compare := flag.Bool("compare", false, "print comparison table from results history and exit")
	flag.Parse()

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
	var results []eval.RunResult
	var skipped []string
	for _, tc := range cases {
		fn, ok := a.outputFunc(tc.Phase)
		if !ok {
			skipped = append(skipped, tc.Phase+"/"+tc.Name)
			continue
		}
		caseRuns := *runs
		if caseRuns < 0 {
			caseRuns = cfg.Eval.RunsPerCase
		}
		if caseRuns == 0 {
			_, total, err := eval.ParseThreshold(tc.PassThreshold)
			if err != nil {
				slog.Error("invalid threshold", "case", tc.Name, "error", err)
				os.Exit(1)
			}
			caseRuns = total
		}

		slog.Info("running case", "case", tc.Phase+"/"+tc.Name, "runs", caseRuns, "model", agentModel)
		start := time.Now()
		result, err := eval.RunCaseE(tc, caseRuns, fn)
		if err != nil {
			slog.Error("run case", "case", tc.Name, "error", err)
			os.Exit(1)
		}
		wall := time.Since(start)
		prompt, comp := a.takeUsage()
		results = append(results, result)

		rec := eval.Record{
			Timestamp: time.Now().UTC(), GitSHA: sha,
			Case: result.Case, Phase: tc.Phase,
			Model: agentModel, ArbiterModel: a.arbModel,
			Runs: result.Runs, Passes: result.Passes,
			Threshold: result.Threshold, Pass: result.Pass,
			PromptTokens: prompt, CompTokens: comp,
			WallTimeSecs: wall.Seconds(), Failures: result.Failures,
		}
		if err := eval.AppendRecord(dir, rec); err != nil {
			slog.Error("append record", "error", err)
			os.Exit(1)
		}
	}

	fmt.Print(eval.FormatReport(results))
	for _, s := range skipped {
		fmt.Printf("SKIPPED  %s (phase not supported in v1 or arbiter not configured)\n", s)
	}

	for _, r := range results {
		if !r.Pass {
			os.Exit(1)
		}
	}
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
