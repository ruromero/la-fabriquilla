package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBenchmark(t *testing.T) {
	dir := t.TempDir()
	models := []string{"model-a", "model-b"}
	results := map[string][]RunResult{
		"model-a": {
			{Case: "planner/case-001", Runs: 10, Passes: 9, Pass: true},
			{Case: "coder/case-001", Runs: 10, Passes: 6, Pass: false},
		},
		"model-b": {
			{Case: "planner/case-001", Runs: 10, Passes: 10, Pass: true},
			{Case: "coder/case-001", Runs: 10, Passes: 10, Pass: true},
		},
	}
	report := FormatModelMatrix(models, results, nil)

	path, err := WriteBenchmark(dir, "planner,coder", "abc1234", models, results, report)
	if err != nil {
		t.Fatalf("WriteBenchmark: %v", err)
	}

	// Benchmark file exists and has expected content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read benchmark file: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"<!-- benchmark:",
		"model-a,model-b",
		"planner,coder",
		"abc1234",
		"runs=10",
		"# Model Comparison",
		"Model Comparison Report",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("benchmark file missing %q", want)
		}
	}

	// README.md index was created.
	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	idx := string(index)
	for _, want := range []string{
		"# Model Benchmarks",
		"model-a,model-b",
		"planner,coder",
		"abc1234",
		filepath.Base(path),
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

func TestWriteBenchmarkEmptyPhase(t *testing.T) {
	dir := t.TempDir()
	models := []string{"model-a"}
	results := map[string][]RunResult{
		"model-a": {{Case: "planner/case-001", Runs: 5, Passes: 5, Pass: true}},
	}
	report := FormatModelMatrix(models, results, nil)
	path, err := WriteBenchmark(dir, "", "sha123", models, results, report)
	if err != nil {
		t.Fatalf("WriteBenchmark: %v", err)
	}
	// The metadata comment in the benchmark file uses "all" for an empty phase.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "phase=all") {
		t.Error("expected phase=all in benchmark file when empty phase passed")
	}
}

func TestRebuildBenchmarkIndexMultipleRuns(t *testing.T) {
	dir := t.TempDir()
	models := []string{"m"}
	results := map[string][]RunResult{
		"m": {{Case: "c", Runs: 3, Passes: 3, Pass: true}},
	}
	report := FormatModelMatrix(models, results, nil)

	if _, err := WriteBenchmark(dir, "coder", "sha1", models, results, report); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteBenchmark(dir, "planner", "sha2", models, results, report); err != nil {
		t.Fatal(err)
	}

	idx, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if strings.Count(string(idx), "| `m` |") != 2 {
		t.Errorf("expected 2 rows in index, got:\n%s", idx)
	}
}

func TestParseBenchmarkMeta(t *testing.T) {
	line := `<!-- benchmark: date=2026-06-15T14:30:00Z models=a,b phase=all sha=abc runs=10 -->`
	m := parseBenchmarkMeta(line)
	checks := map[string]string{
		"date":   "2026-06-15T14:30:00Z",
		"models": "a,b",
		"phase":  "all",
		"sha":    "abc",
		"runs":   "10",
	}
	for k, want := range checks {
		if got := m[k]; got != want {
			t.Errorf("meta[%q] = %q, want %q", k, got, want)
		}
	}
}
