package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Record is one persisted eval result: a single case run to completion
// against a specific model. Appended to {dir}/{YYYY-MM}.jsonl.
type Record struct {
	Timestamp    time.Time `json:"timestamp"`
	GitSHA       string    `json:"git_sha"`
	Case         string    `json:"case"`
	Phase        string    `json:"phase"`
	Model        string    `json:"model"`
	ArbiterModel string    `json:"arbiter_model,omitempty"`
	Runs         int       `json:"runs"`
	Passes       int       `json:"passes"`
	Threshold    int       `json:"threshold"`
	Pass         bool      `json:"pass"`
	PromptTokens int       `json:"prompt_tokens"`
	CompTokens   int       `json:"completion_tokens"`
	WallTimeSecs float64   `json:"wall_time_seconds"`
	Failures     []string  `json:"failures,omitempty"`
}

// AppendRecord appends rec as one JSON line to the monthly results file.
func AppendRecord(dir string, rec Record) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("results dir: %w", err)
	}
	name := rec.Timestamp.UTC().Format("2006-01") + ".jsonl"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open results file: %w", err)
	}
	defer f.Close()
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write record: %w", err)
	}
	return nil
}

// LoadRecords reads every record from all .jsonl files in dir.
func LoadRecords(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read results dir: %w", err)
	}
	var records []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", e.Name(), err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var rec Record
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				f.Close()
				return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
			}
			records = append(records, rec)
		}
		if err := sc.Err(); err != nil {
			f.Close()
			return nil, fmt.Errorf("scan %s: %w", e.Name(), err)
		}
		f.Close()
	}
	return records, nil
}

// FormatComparison renders a pass-rate table using the latest record per
// case × model pair.
func FormatComparison(records []Record) string {
	type key struct{ caseName, model string }
	latest := map[key]Record{}
	for _, r := range records {
		k := key{r.Case, r.Model}
		if cur, ok := latest[k]; !ok || r.Timestamp.After(cur.Timestamp) {
			latest[k] = r
		}
	}
	keys := make([]key, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].caseName != keys[j].caseName {
			return keys[i].caseName < keys[j].caseName
		}
		return keys[i].model < keys[j].model
	})

	var b strings.Builder
	b.WriteString("Model Comparison (latest record per case x model)\n")
	b.WriteString("=================================================\n\n")
	fmt.Fprintf(&b, "%-45s %-25s %-8s %-6s %s\n", "CASE", "MODEL", "RATE", "PASS", "WALL")
	for _, k := range keys {
		r := latest[k]
		status := "ok"
		if !r.Pass {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "%-45s %-25s %d/%d     %-6s %.1fs\n",
			r.Case, r.Model, r.Passes, r.Runs, status, r.WallTimeSecs)
	}
	return b.String()
}
