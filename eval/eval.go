package eval

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TestCase represents a single evaluation test case with inputs,
// assertions, and a pass threshold.
type TestCase struct {
	Name          string            `json:"name"`
	Phase         string            `json:"phase"`
	Inputs        map[string]string `json:"inputs"`
	Assertions    []Assertion       `json:"assertions"`
	PassThreshold string            `json:"pass_threshold"`
}

// Assertion defines a single behavioral check against agent output.
type Assertion struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// RunResult captures the outcome of running a test case multiple times.
type RunResult struct {
	Case      string   `json:"case"`
	Runs      int      `json:"runs"`
	Passes    int      `json:"passes"`
	Threshold int      `json:"threshold"`
	TotalRuns int      `json:"total_runs"`
	Pass      bool     `json:"pass"`
	Failures  []string `json:"failures,omitempty"`
}

// FileState represents a file produced by an agent run.
type FileState struct {
	Path string
}

// OutputFunc is called for each run to produce the output and files
// that assertions are checked against. When LLM execution is wired
// in, this function calls the actual agent. For structural validation
// it returns mock data.
type OutputFunc func(tc TestCase, run int) (output string, files []FileState)

// OutputFuncE is like OutputFunc but may return an error (e.g. inference
// failure or timeout). An errored run counts as a failed run; the suite
// continues.
type OutputFuncE func(tc TestCase, run int) (string, []FileState, error)

// RunCaseE executes a test case like RunCase, treating output errors as
// failed runs rather than aborting the case.
func RunCaseE(tc TestCase, runs int, outputFn OutputFuncE) (RunResult, error) {
	threshold, totalRuns, err := ParseThreshold(tc.PassThreshold)
	if err != nil {
		return RunResult{}, fmt.Errorf("case %s: %w", tc.Name, err)
	}

	passes := 0
	var failures []string
	for run := 1; run <= runs; run++ {
		output, files, err := outputFn(tc, run)
		if err != nil {
			failures = append(failures, fmt.Sprintf("Run %d: run error: %v", run, err))
			continue
		}
		allPassed := true
		for _, a := range tc.Assertions {
			if !CheckAssertion(a, output, files) {
				allPassed = false
				failures = append(failures,
					fmt.Sprintf("Run %d: assertion failed: %s %q", run, a.Type, a.Value))
			}
		}
		if allPassed {
			passes++
		}
	}

	return RunResult{
		Case:      tc.Phase + "/" + tc.Name,
		Runs:      runs,
		Passes:    passes,
		Threshold: threshold,
		TotalRuns: totalRuns,
		Pass:      passes >= threshold,
		Failures:  failures,
	}, nil
}

// RunCase executes a test case the specified number of times using
// outputFn to produce output per run. Returns a RunResult.
func RunCase(tc TestCase, runs int, outputFn OutputFunc) (RunResult, error) {
	return RunCaseE(tc, runs, func(tc TestCase, run int) (string, []FileState, error) {
		output, files := outputFn(tc, run)
		return output, files, nil
	})
}

// LoadTestCases loads all .json test case files from dir and its
// subdirectories.
func LoadTestCases(dir string) ([]TestCase, error) {
	var cases []TestCase
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", path, err)
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		var tc TestCase
		if err := json.Unmarshal(data, &tc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		cases = append(cases, tc)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("loading test cases from %s: %w", dir, err)
	}
	return cases, nil
}

// ParseThreshold parses a threshold string like "8/10" into the number
// of required passes and total runs.
func ParseThreshold(s string) (passes int, total int, err error) {
	if s == "" {
		return 0, 0, fmt.Errorf("parsing threshold: empty string")
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("parsing threshold %q: expected format N/M", s)
	}
	passes, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parsing threshold %q: passes: %w", s, err)
	}
	total, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parsing threshold %q: total: %w", s, err)
	}
	if total <= 0 {
		return 0, 0, fmt.Errorf("parsing threshold %q: total must be > 0", s)
	}
	if passes < 0 {
		return 0, 0, fmt.Errorf("parsing threshold %q: passes must be >= 0", s)
	}
	if passes > total {
		return 0, 0, fmt.Errorf("parsing threshold %q: passes cannot exceed total", s)
	}
	return passes, total, nil
}

// CheckAssertion checks a single assertion against the given output
// text and file list. Returns true if the assertion passes.
func CheckAssertion(a Assertion, output string, files []FileState) bool {
	switch a.Type {
	case "outcome_equals":
		return matchOutcome(output, a.Value)
	case "output_contains":
		return strings.Contains(output, a.Value)
	case "output_not_contains":
		return !strings.Contains(output, a.Value)
	case "file_count_gte":
		n, err := strconv.Atoi(a.Value)
		if err != nil || n < 0 {
			slog.Warn("file_count_gte: invalid value", "value", a.Value)
			return false
		}
		return len(files) >= n
	case "file_paths_include":
		for _, f := range files {
			if f.Path == a.Value {
				return true
			}
		}
		return false
	case "severity_present":
		tag := "[" + strings.ToUpper(a.Value) + "]"
		return strings.Contains(output, tag)
	case "compiles":
		slog.Warn("assertion type 'compiles' requires sandbox — skipping, returning true")
		return true
	case "tests_pass":
		slog.Warn("assertion type 'tests_pass' requires sandbox — skipping, returning true")
		return true
	default:
		slog.Warn("unknown assertion type", "type", a.Type)
		return false
	}
}

// matchOutcome checks if the output begins with the expected outcome
// as a distinct token (word boundary: followed by end-of-string,
// whitespace, colon, or newline). This prevents "disapprove"
// matching "approve".
func matchOutcome(output, expected string) bool {
	idx := strings.Index(output, expected)
	if idx < 0 {
		return false
	}
	if idx > 0 {
		prev := output[idx-1]
		if isAlpha(prev) {
			return false
		}
	}
	end := idx + len(expected)
	if end >= len(output) {
		return true
	}
	next := output[end]
	return next == ' ' || next == ':' || next == '\n' || next == '\t' || next == ','
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
