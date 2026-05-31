package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
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

func ReviewNeedsIteration(correctness, security, intent string) bool {
	combined := correctness + security + intent
	return strings.Contains(combined, "[CRITICAL]") || strings.Contains(combined, "[MEDIUM]")
}

func FormatReviewFeedback(correctness, security, intent string) string {
	return fmt.Sprintf("## Correctness Review\n\n%s\n\n## Security Review\n\n%s\n\n## Intent Review\n\n%s",
		correctness, security, intent)
}
