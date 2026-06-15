package eval

import (
	"fmt"
	"strings"
)

// FormatModelMatrix renders a side-by-side comparison of eval results across
// multiple models. models gives the column order. results maps model name to
// run results for that model. skipped lists phase/case names skipped for all
// models (same across models since skip logic depends on adapter config, not
// model name).
func FormatModelMatrix(models []string, results map[string][]RunResult, skipped []string) string {
	if len(models) == 0 {
		return ""
	}

	// Case order from first model's results.
	firstResults := results[models[0]]
	caseOrder := make([]string, 0, len(firstResults))
	for _, r := range firstResults {
		caseOrder = append(caseOrder, r.Case)
	}

	// Build per-model lookup: case → RunResult.
	byModel := make(map[string]map[string]RunResult, len(models))
	for _, m := range models {
		byModel[m] = make(map[string]RunResult, len(results[m]))
		for _, r := range results[m] {
			byModel[m][r.Case] = r
		}
	}

	// Column width = max(modelName, len("PASS (10/10)")) = max(len(m), 12).
	const caseWidth = 45
	const minColWidth = 12 // "PASS (10/10)" is 12 chars
	colW := make([]int, len(models))
	for i, m := range models {
		if w := len(m); w > minColWidth {
			colW[i] = w
		} else {
			colW[i] = minColWidth
		}
	}

	var b strings.Builder
	b.WriteString("Model Comparison Report\n")
	b.WriteString("=======================\n\n")

	// Header row.
	b.WriteString(fmt.Sprintf("%-*s  ", caseWidth, ""))
	for i, m := range models {
		b.WriteString(fmt.Sprintf("%-*s  ", colW[i], m))
	}
	b.WriteString("\n")

	// Data rows.
	for _, name := range caseOrder {
		b.WriteString(fmt.Sprintf("%-*s  ", caseWidth, name))
		for i, m := range models {
			var cell string
			if r, ok := byModel[m][name]; ok {
				status := "PASS"
				if !r.Pass {
					status = "FAIL"
				}
				cell = fmt.Sprintf("%s (%d/%d)", status, r.Passes, r.Runs)
			} else {
				cell = "???"
			}
			b.WriteString(fmt.Sprintf("%-*s  ", colW[i], cell))
		}
		b.WriteString("\n")
	}

	// Skipped rows.
	for _, s := range skipped {
		b.WriteString(fmt.Sprintf("%-*s  ", caseWidth, s))
		for i := range models {
			b.WriteString(fmt.Sprintf("%-*s  ", colW[i], "SKIPPED"))
		}
		b.WriteString("\n")
	}

	// Summary.
	b.WriteString("\nSummary\n-------\n")
	for _, m := range models {
		passed, total := 0, len(results[m])
		for _, r := range results[m] {
			if r.Pass {
				passed++
			}
		}
		b.WriteString(fmt.Sprintf("%-25s %d/%d passed\n", m, passed, total))
	}
	return b.String()
}

// FormatReport produces a human-readable evaluation summary from the
// given run results. Skipped lists phase/case names skipped because the
// phase is unsupported or unconfigured.
func FormatReport(results []RunResult, skipped []string) string {
	var b strings.Builder
	b.WriteString("Golden-Set Evaluation Report\n")
	b.WriteString("============================\n\n")

	passed := 0
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
		} else {
			passed++
		}
		b.WriteString(fmt.Sprintf("%-45s %s  (%d/%d, threshold %d/%d)\n",
			r.Case, status, r.Passes, r.Runs, r.Threshold, r.TotalRuns))

		for _, f := range r.Failures {
			b.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	for _, s := range skipped {
		b.WriteString(fmt.Sprintf("%-45s SKIPPED\n", s))
	}

	b.WriteString(fmt.Sprintf("\nOverall: %d/%d passed\n", passed, len(results)))
	return b.String()
}
