package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/ruromero/la-fabriquilla/eval"
)

func main() {
	dir := flag.String("dir", "tests/golden", "path to golden-set directory")
	runs := flag.Int("runs", 0, "number of runs per test case (0 = use threshold denominator)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if *runs < 0 {
		slog.Error("--runs must be >= 0")
		os.Exit(1)
	}

	cases, err := eval.LoadTestCases(*dir)
	if err != nil {
		slog.Error("failed to load test cases", "dir", *dir, "error", err)
		os.Exit(1)
	}

	if len(cases) == 0 {
		slog.Warn("no test cases found", "dir", *dir)
		os.Exit(0)
	}

	slog.Info("loaded test cases", "count", len(cases))

	mockOutputFn := func(tc eval.TestCase, run int) (string, []eval.FileState) {
		return buildMockOutput(tc), buildMockFiles(tc)
	}

	var results []eval.RunResult
	for _, tc := range cases {
		caseRuns := *runs
		if caseRuns == 0 {
			_, total, err := eval.ParseThreshold(tc.PassThreshold)
			if err != nil {
				slog.Error("invalid threshold", "case", tc.Name, "error", err)
				os.Exit(1)
			}
			caseRuns = total
		}

		result, err := eval.RunCase(tc, caseRuns, mockOutputFn)
		if err != nil {
			slog.Error("failed to run case", "case", tc.Name, "error", err)
			os.Exit(1)
		}
		results = append(results, result)
	}

	fmt.Print(eval.FormatReport(results, nil))

	for _, r := range results {
		if !r.Pass {
			os.Exit(1)
		}
	}
}

// buildMockOutput creates a synthetic output that satisfies the test
// case's assertions for structural validation. When LLM execution is
// wired in, this will be replaced by actual model output.
func buildMockOutput(tc eval.TestCase) string {
	var parts []string

	for _, a := range tc.Assertions {
		switch a.Type {
		case "outcome_equals":
			parts = append(parts, a.Value+":")
		case "output_contains":
			parts = append(parts, a.Value)
		case "severity_present":
			parts = append(parts, "["+a.Value+"]")
		}
	}

	return fmt.Sprintf("%s\n", joinParts(parts))
}

// buildMockFiles creates a synthetic file list that satisfies
// file-related assertions.
func buildMockFiles(tc eval.TestCase) []eval.FileState {
	var files []eval.FileState
	maxCount := 0

	for _, a := range tc.Assertions {
		switch a.Type {
		case "file_paths_include":
			files = append(files, eval.FileState{Path: a.Value})
		case "file_count_gte":
			n := 0
			fmt.Sscanf(a.Value, "%d", &n)
			if n > maxCount {
				maxCount = n
			}
		}
	}

	for len(files) < maxCount {
		files = append(files, eval.FileState{
			Path: fmt.Sprintf("mock/file_%d.go", len(files)),
		})
	}

	return files
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}
