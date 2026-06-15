package eval

import (
	"encoding/json"
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
	base := now.Format("2006-01-02T150405")

	runsPerCase := 0
	if len(models) > 0 && len(results[models[0]]) > 0 {
		runsPerCase = results[models[0]][0].Runs
	}

	phaseLabel := phase
	if phaseLabel == "" {
		phaseLabel = "all"
	}
	modelsCSV := strings.Join(models, ",")

	metaJSON, _ := json.Marshal(map[string]any{
		"date":   now.Format(time.RFC3339),
		"models": modelsCSV,
		"phase":  phaseLabel,
		"sha":    sha,
	})
	meta := fmt.Sprintf("%s %s -->", benchmarkMetaPrefix, string(metaJSON))

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

	// Atomically create the file; retry with a numeric suffix on collision.
	// O_EXCL guarantees no TOCTOU race between concurrent eval-runner processes.
	filename := base + ".md"
	var f *os.File
	for i := 1; ; i++ {
		var openErr error
		f, openErr = os.OpenFile(filepath.Join(dir, filename), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			break
		}
		if !os.IsExist(openErr) {
			return "", fmt.Errorf("create benchmark file: %w", openErr)
		}
		filename = fmt.Sprintf("%s-%d.md", base, i)
	}
	path := filepath.Join(dir, filename)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", fmt.Errorf("write benchmark file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close benchmark file: %w", err)
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
			sha:    m["sha"],
		})
	}

	var b strings.Builder
	b.WriteString("# Model Benchmarks\n\n")
	if len(rows) == 0 {
		b.WriteString("No benchmark runs yet.\n")
	} else {
		b.WriteString("| Date (UTC) | Models | Phase | SHA | Report |\n")
		b.WriteString("|------------|--------|-------|-----|--------|\n")
		for _, r := range rows {
			b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s | [view](%s) |\n",
				r.date, r.models, r.phase, r.sha, r.file))
		}
	}

	return os.WriteFile(filepath.Join(dir, benchmarkIndexFile), []byte(b.String()), 0o644)
}

// parseBenchmarkMeta parses the structured metadata comment at the top of a
// benchmark file: <!-- benchmark: {"key":"value",...} -->
func parseBenchmarkMeta(line string) map[string]string {
	line = strings.TrimPrefix(line, benchmarkMetaPrefix)
	line = strings.TrimSuffix(strings.TrimSpace(line), "-->")
	line = strings.TrimSpace(line)
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}
