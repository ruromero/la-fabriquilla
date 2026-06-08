package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruromero/la-fabriquilla/review"
)

// coderOutputSchema is the JSON Schema for structured coder output.
var coderOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"files": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":     map[string]any{"type": "string"},
					"language": map[string]any{"type": "string"},
					"content":  map[string]any{"type": "string"},
				},
				"required": []string{"path", "content"},
			},
		},
	},
	"required": []string{"files"},
}

type coderOutput struct {
	Files []struct {
		Path     string `json:"path"`
		Language string `json:"language"`
		Content  string `json:"content"`
	} `json:"files"`
}

// GetCoderOutputSchema returns a copy of the JSON Schema for structured coder output.
func GetCoderOutputSchema() map[string]any {
	cp := make(map[string]any, len(coderOutputSchema))
	for k, v := range coderOutputSchema {
		cp[k] = v
	}
	return cp
}

// ParseStructuredCodeOutput parses JSON structured coder output into a FileState slice.
func ParseStructuredCodeOutput(jsonOutput string) ([]FileState, error) {
	var out coderOutput
	if err := json.Unmarshal([]byte(jsonOutput), &out); err != nil {
		return nil, err
	}
	files := make([]FileState, 0, len(out.Files))
	for _, f := range out.Files {
		if f.Path == "" {
			return nil, fmt.Errorf("structured output contains file with empty path")
		}
		files = append(files, FileState{Path: f.Path, Content: f.Content})
	}
	return files, nil
}

func ParseCodeOutput(output string) []FileState {
	var files []FileState
	lines := strings.Split(output, "\n")
	var currentPath string
	var content strings.Builder
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FILE:") {
			currentPath = strings.TrimSpace(strings.TrimPrefix(trimmed, "FILE:"))
			continue
		}
		if !inBlock && strings.HasPrefix(trimmed, "```") && currentPath != "" {
			inBlock = true
			content.Reset()
			continue
		}
		if inBlock && trimmed == "```" {
			files = append(files, FileState{
				Path:    currentPath,
				Content: strings.TrimRight(content.String(), "\n"),
			})
			currentPath = ""
			inBlock = false
			continue
		}
		if inBlock {
			content.WriteString(line)
			content.WriteString("\n")
		}
	}
	return files
}

func ReviewNeedsIteration(findings []review.ReviewFinding) bool {
	for _, f := range findings {
		if f.Severity == review.SeverityCritical || f.Severity == review.SeverityMedium {
			return true
		}
	}
	return false
}

func FormatReviewFeedback(findings []review.ReviewFinding) string {
	if len(findings) == 0 {
		return "[PASS] No issues found."
	}
	var b strings.Builder
	for _, f := range findings {
		tag := strings.ToUpper(string(f.Severity))
		fmt.Fprintf(&b, "[%s] %s", tag, f.Title)
		if f.Detail != "" {
			fmt.Fprintf(&b, " — %s", f.Detail)
		}
		if f.File != "" {
			fmt.Fprintf(&b, ", %s", f.File)
			if f.Line > 0 {
				fmt.Fprintf(&b, ":%d", f.Line)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func ArbiterNeedsIteration(findings []review.ArbiterFinding) bool {
	for _, f := range findings {
		if f.Classification == review.ClassFixHere || f.Classification == review.ClassSubtask {
			return true
		}
	}
	return false
}

// EffectiveFindings returns the subset of review findings that should
// be acted on after arbiter classification. Only findings classified
// as fix_here or subtask are returned.
func EffectiveFindings(arbiter *ArbiterState, raw *ReviewState) []review.ReviewFinding {
	if arbiter == nil || len(arbiter.Findings) == 0 {
		if raw == nil {
			return nil
		}
		return raw.Findings
	}
	var effective []review.ReviewFinding
	for _, af := range arbiter.Findings {
		if af.Classification == review.ClassFixHere || af.Classification == review.ClassSubtask {
			effective = append(effective, af.Finding)
		}
	}
	return effective
}
