package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const benchmarkMetaPrefix = "<!-- benchmark:"
const benchmarkIndexFile = "README.md"

// WriteBenchmark writes a dated markdown comparison report to dir and
// regenerates the README.md index. Returns the path of the written file.
//
// phase is the -phase flag value (empty string means all phases).
// models gives the column order; results is the per-model RunResult map.
func WriteBenchmark(dir, phase, sha string, models []string, results map[string][]RunResult, report string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create benchmark dir: %w", err)
	}

	now := time.Now().UTC()
	filename := now.Format("2006-01-02T150405.000") + ".md"
	path := filepath.Join(dir, filename)
	// Avoid collision if multiple runs start within the same millisecond.
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		filename = now.Format("2006-01-02T150405.000") + fmt.Sprintf("-%d", i) + ".md"
		path = filepath.Join(dir, filename)
	}

	runsPerCase := 0
	if len(models) > 0 && len(results[models[0]]) > 0 {
		runsPerCase = results[models[0]][0].Runs
	}

	phaseLabel := phase
	if phaseLabel == "" {
		phaseLabel = "all"
	}
	modelsCSV := strings.Join(models, ",")

	meta := fmt.Sprintf("%s date=%s models=%s phase=%s sha=%s runs=%d -->",
		benchmarkMetaPrefix,
		now.Format(time.RFC3339),
		modelsCSV,
		phaseLabel,
		sha,
		runsPerCase,
	)

	content := fmt.Sprintf(
		"%s\n# Model Comparison — %s UTC\n\n**Models:** %s  \n**Phase:** %s  \n**Runs per case:** %d  \n**SHA:** %s\n\n```\n%s```\n",
		meta,
		now.Format("2006-01-02 15:04"),
		strings.Join(models, ", "),
		phaseLabel,
		runsPerCase,
		sha,
		report,
	)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write benchmark file: %w", err)
	}

	if err := rebuildBenchmarkIndex(dir); err != nil {
		return path, fmt.Errorf("update benchmark index: %w", err)
	}
	return path, nil
}

// rebuildBenchmarkIndex scans dir for benchmark markdown files and
// regenerates README.md with a table of all runs.
func rebuildBenchmarkIndex(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	type row struct {
		file   string
		date   string
		models string
		phase  string
		runs   string
		sha    string
	}

	var rows []row
	for _, e := range entries {
		if e.IsDir() || e.Name() == benchmarkIndexFile || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		first, _, _ := strings.Cut(string(data), "\n")
		if !strings.HasPrefix(first, benchmarkMetaPrefix) {
			continue
		}
		m := parseBenchmarkMeta(first)
		rows = append(rows, row{
			file:   e.Name(),
			date:   m["date"],
			models: m["models"],
			phase:  m["phase"],
			runs:   m["runs"],
			sha:    m["sha"],
		})
	}

	var b strings.Builder
	b.WriteString("# Model Benchmarks\n\n")
	if len(rows) == 0 {
		b.WriteString("No benchmark runs yet.\n")
	} else {
		b.WriteString("| Date (UTC) | Models | Phase | Runs | SHA | Report |\n")
		b.WriteString("|------------|--------|-------|------|-----|--------|\n")
		for _, r := range rows {
			b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s | %s | [view](%s) |\n",
				r.date, r.models, r.phase, r.runs, r.sha, r.file))
		}
	}

	return os.WriteFile(filepath.Join(dir, benchmarkIndexFile), []byte(b.String()), 0o644)
}

// parseBenchmarkMeta parses the structured metadata comment at the top of a
// benchmark file: <!-- benchmark: key=value key=value -->
func parseBenchmarkMeta(line string) map[string]string {
	line = strings.TrimPrefix(line, benchmarkMetaPrefix)
	line = strings.TrimSuffix(strings.TrimSpace(line), "-->")
	result := make(map[string]string)
	for _, kv := range strings.Fields(strings.TrimSpace(line)) {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			result[k] = v
		}
	}
	return result
}
