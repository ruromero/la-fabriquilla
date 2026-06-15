package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseThreshold(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		tests := []struct {
			input     string
			wantPass  int
			wantTotal int
		}{
			{"8/10", 8, 10},
			{"1/1", 1, 1},
			{"0/5", 0, 5},
			{"10/10", 10, 10},
		}
		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				p, tot, err := ParseThreshold(tt.input)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if p != tt.wantPass {
					t.Errorf("passes = %d, want %d", p, tt.wantPass)
				}
				if tot != tt.wantTotal {
					t.Errorf("total = %d, want %d", tot, tt.wantTotal)
				}
			})
		}
	})

	t.Run("invalid", func(t *testing.T) {
		tests := []string{
			"abc",
			"8/0",
			"",
			"8",
			"/10",
			"11/10",
			"-1/10",
		}
		for _, input := range tests {
			t.Run(input, func(t *testing.T) {
				_, _, err := ParseThreshold(input)
				if err == nil {
					t.Fatalf("expected error for %q, got nil", input)
				}
			})
		}
	})
}

func TestCheckAssertion(t *testing.T) {
	t.Run("outcome_equals with word boundary", func(t *testing.T) {
		a := Assertion{Type: "outcome_equals", Value: "plan"}
		if !CheckAssertion(a, "plan: fix the nil pointer", nil) {
			t.Error("expected pass when output starts with value followed by colon")
		}
		if !CheckAssertion(a, "plan\nmore details", nil) {
			t.Error("expected pass when output starts with value followed by newline")
		}
		if CheckAssertion(a, "needs_info: missing details", nil) {
			t.Error("expected fail when output does not contain value")
		}
	})

	t.Run("outcome_equals prevents substring false positives", func(t *testing.T) {
		a := Assertion{Type: "outcome_equals", Value: "approve"}
		if CheckAssertion(a, "disapprove: this is wrong", nil) {
			t.Error("expected fail: 'disapprove' should not match 'approve'")
		}
		if !CheckAssertion(a, "approve: looks good", nil) {
			t.Error("expected pass: 'approve:' should match")
		}
	})

	t.Run("output_contains", func(t *testing.T) {
		a := Assertion{Type: "output_contains", Value: "func NewParser"}
		if !CheckAssertion(a, "added func NewParser to parser.go", nil) {
			t.Error("expected pass")
		}
		if CheckAssertion(a, "added a new function", nil) {
			t.Error("expected fail")
		}
	})

	t.Run("output_not_contains", func(t *testing.T) {
		a := Assertion{Type: "output_not_contains", Value: "NEEDS_INFO"}
		if !CheckAssertion(a, "plan: everything clear", nil) {
			t.Error("expected pass when substring absent")
		}
		if CheckAssertion(a, "NEEDS_INFO: clarification needed", nil) {
			t.Error("expected fail when substring present")
		}
	})

	t.Run("file_count_gte", func(t *testing.T) {
		files := []FileState{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}}
		a := Assertion{Type: "file_count_gte", Value: "2"}
		if !CheckAssertion(a, "", files) {
			t.Error("expected pass: 3 >= 2")
		}
		a.Value = "5"
		if CheckAssertion(a, "", files) {
			t.Error("expected fail: 3 < 5")
		}
		a.Value = "bad"
		if CheckAssertion(a, "", files) {
			t.Error("expected fail for invalid value")
		}
	})

	t.Run("file_count_gte rejects negative", func(t *testing.T) {
		a := Assertion{Type: "file_count_gte", Value: "-1"}
		if CheckAssertion(a, "", nil) {
			t.Error("expected fail for negative value")
		}
	})

	t.Run("file_paths_include", func(t *testing.T) {
		files := []FileState{{Path: "parser/parser.go"}, {Path: "main.go"}}
		a := Assertion{Type: "file_paths_include", Value: "parser/parser.go"}
		if !CheckAssertion(a, "", files) {
			t.Error("expected pass")
		}
		a.Value = "missing.go"
		if CheckAssertion(a, "", files) {
			t.Error("expected fail")
		}
	})

	t.Run("severity_present", func(t *testing.T) {
		a := Assertion{Type: "severity_present", Value: "CRITICAL"}
		if !CheckAssertion(a, "Found issue [CRITICAL] nil dereference", nil) {
			t.Error("expected pass")
		}
		if CheckAssertion(a, "Found issue [MEDIUM] naming", nil) {
			t.Error("expected fail")
		}
	})

	t.Run("compiles_skipped", func(t *testing.T) {
		a := Assertion{Type: "compiles", Value: ""}
		if !CheckAssertion(a, "", nil) {
			t.Error("compiles should return true (skipped)")
		}
	})

	t.Run("tests_pass_skipped", func(t *testing.T) {
		a := Assertion{Type: "tests_pass", Value: ""}
		if !CheckAssertion(a, "", nil) {
			t.Error("tests_pass should return true (skipped)")
		}
	})

	t.Run("unknown_type", func(t *testing.T) {
		a := Assertion{Type: "nonexistent", Value: "x"}
		if CheckAssertion(a, "anything", nil) {
			t.Error("unknown type should return false")
		}
	})
}

func TestRunCase(t *testing.T) {
	tc := TestCase{
		Name:          "test-run-case",
		Phase:         "planner",
		Assertions:    []Assertion{{Type: "output_contains", Value: "hello"}},
		PassThreshold: "2/3",
	}

	passingFn := func(tc TestCase, run int) (string, []FileState) {
		return "hello world", nil
	}

	result, err := RunCase(tc, 3, passingFn)
	if err != nil {
		t.Fatalf("RunCase: %v", err)
	}
	if result.Passes != 3 {
		t.Errorf("Passes = %d, want 3", result.Passes)
	}
	if !result.Pass {
		t.Error("expected Pass = true")
	}
	if result.Threshold != 2 {
		t.Errorf("Threshold = %d, want 2", result.Threshold)
	}
}

func TestLoadTestCases(t *testing.T) {
	t.Run("loads_valid_files", func(t *testing.T) {
		dir := t.TempDir()

		tc := TestCase{
			Name:          "test-case-1",
			Phase:         "planner",
			Inputs:        map[string]string{"issue_title": "Fix bug"},
			Assertions:    []Assertion{{Type: "output_contains", Value: "fix"}},
			PassThreshold: "8/10",
		}
		data, err := json.Marshal(tc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "case-001.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644); err != nil {
			t.Fatal(err)
		}

		cases, err := LoadTestCases(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cases) != 1 {
			t.Fatalf("got %d cases, want 1", len(cases))
		}
		if cases[0].Name != "test-case-1" {
			t.Errorf("name = %q, want %q", cases[0].Name, "test-case-1")
		}
	})

	t.Run("loads_subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		subdir := filepath.Join(dir, "planner")
		if err := os.Mkdir(subdir, 0755); err != nil {
			t.Fatal(err)
		}

		tc := TestCase{
			Name:          "sub-case",
			Phase:         "planner",
			Inputs:        map[string]string{},
			Assertions:    []Assertion{},
			PassThreshold: "1/1",
		}
		data, err := json.Marshal(tc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "case.json"), data, 0644); err != nil {
			t.Fatal(err)
		}

		cases, err := LoadTestCases(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cases) != 1 {
			t.Fatalf("got %d cases, want 1", len(cases))
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{invalid"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTestCases(dir)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("nonexistent_dir", func(t *testing.T) {
		_, err := LoadTestCases("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Fatal("expected error for nonexistent directory")
		}
	})
}

func TestFormatReport(t *testing.T) {
	results := []RunResult{
		{
			Case:      "planner/case-001",
			Runs:      10,
			Passes:    10,
			Threshold: 8,
			TotalRuns: 10,
			Pass:      true,
		},
		{
			Case:      "coder/case-001",
			Runs:      10,
			Passes:    6,
			Threshold: 8,
			TotalRuns: 10,
			Pass:      false,
			Failures: []string{
				`Run 3: assertion failed: output_contains "func NewParser"`,
			},
		},
	}

	report := FormatReport(results, nil)
	if report == "" {
		t.Fatal("report should not be empty")
	}
	for _, want := range []string{"Golden-Set Evaluation Report", "PASS", "FAIL", "1/2 passed", `output_contains "func NewParser"`} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestRunCaseEErrorCountsAsFailedRun(t *testing.T) {
	tc := TestCase{
		Name:          "errcase",
		Phase:         "planner",
		Assertions:    []Assertion{{Type: "output_contains", Value: "x"}},
		PassThreshold: "1/2",
	}
	calls := 0
	fn := func(tc TestCase, run int) (string, []FileState, error) {
		calls++
		if run == 1 {
			return "", nil, fmt.Errorf("inference timeout")
		}
		return "x", nil, nil
	}
	res, err := RunCaseE(tc, 2, fn)
	if err != nil {
		t.Fatalf("RunCaseE: %v", err)
	}
	if calls != 2 {
		t.Errorf("suite stopped early: %d calls", calls)
	}
	if res.Passes != 1 || !res.Pass {
		t.Errorf("passes=%d pass=%v, want 1/true", res.Passes, res.Pass)
	}
	found := false
	for _, f := range res.Failures {
		if strings.Contains(f, "inference timeout") {
			found = true
		}
	}
	if !found {
		t.Errorf("error not recorded in failures: %v", res.Failures)
	}
}
