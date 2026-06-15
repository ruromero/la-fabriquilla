package eval

import (
	"fmt"
	"strings"
)

// FormatModelMatrix renders a side-by-side comparison of eval results across
// multiple models. models gives the column order. results maps model name to
// run results for that model. skipped lists phase/case names skipped for all
// models (same across models since skip logic depends on adapter config, not
// model name). pricing maps model name to [inputPricePerM, outputPricePerM]
// in USD; omit a model or pass nil to skip cost display for that model.
func FormatModelMatrix(models []string, results map[string][]RunResult, skipped []string, pricing map[string][2]float64) string {
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

	// Column width = max(modelName, widest cell seen in data).
	const caseWidth = 45
	maxCellLen := len("SKIPPED") // minimum floor
	for _, m := range models {
		for _, r := range results[m] {
			if w := len(fmt.Sprintf("PASS (%d/%d)", r.Runs, r.Runs)); w > maxCellLen {
				maxCellLen = w
			}
		}
	}
	colW := make([]int, len(models))
	for i, m := range models {
		if w := len(m); w > maxCellLen {
			colW[i] = w
		} else {
			colW[i] = maxCellLen
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

	// Summary: cases passed, avg pass rate, avg wall time, tokens, cost.
	b.WriteString("\nSummary\n-------\n")
	for _, m := range models {
		casesPassed, totalCases := 0, len(results[m])
		totalPasses, totalRuns := 0, 0
		totalWall := 0.0
		totalPrompt, totalComp := 0, 0
		for _, r := range results[m] {
			if r.Pass {
				casesPassed++
			}
			totalPasses += r.Passes
			totalRuns += r.Runs
			totalWall += r.WallSecs
			totalPrompt += r.PromptTokens
			totalComp += r.CompTokens
		}
		rate := 0.0
		if totalRuns > 0 {
			rate = float64(totalPasses) / float64(totalRuns) * 100
		}
		avgWall := 0.0
		if totalCases > 0 {
			avgWall = totalWall / float64(totalCases)
		}
		totalTok := totalPrompt + totalComp
		line := fmt.Sprintf("%-25s %d/%d cases  avg %.0f%% pass rate", m, casesPassed, totalCases, rate)
		if totalWall > 0 {
			line += fmt.Sprintf("  avg %.0fs/case", avgWall)
		}
		if totalTok > 0 {
			line += fmt.Sprintf("  %.1fK tok", float64(totalTok)/1000)
		}
		if p, ok := pricing[m]; ok && (p[0] > 0 || p[1] > 0) {
			cost := float64(totalPrompt)/1_000_000*p[0] + float64(totalComp)/1_000_000*p[1]
			line += fmt.Sprintf("  $%.4f", cost)
		}
		b.WriteString(line + "\n")
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
