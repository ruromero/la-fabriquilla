package review

import (
	"regexp"
	"strconv"
	"strings"
)

// fileRefPattern matches patterns like path/file.go:42 or path/file.go:42-99
var fileRefPattern = regexp.MustCompile(`([\w/.+-]+\.\w+):(\d+)(?:-\d+)?`)

// ParseReviewFindings parses correctness/security pass output.
// category is applied to all findings from this pass.
func ParseReviewFindings(text string, category Category) []ReviewFinding {
	if text == "" {
		return nil
	}
	var findings []ReviewFinding
	for _, block := range splitFindings(text) {
		sev, rest := extractSeverityMarker(block)
		if sev == "" {
			continue
		}
		title, detail := splitTitleDetail(rest)
		file, line := extractFileRef(block)
		findings = append(findings, ReviewFinding{
			Source:   "factory",
			Severity: sev,
			Category: category,
			Title:    title,
			Detail:   detail,
			File:     file,
			Line:     line,
		})
	}
	return findings
}

// ParseIntentFindings parses intent pass output using marker-to-category mapping.
func ParseIntentFindings(text string) []ReviewFinding {
	if text == "" {
		return nil
	}
	var findings []ReviewFinding
	for _, block := range splitFindings(text) {
		trimmed := strings.TrimSpace(block)
		var sev Severity
		var cat Category
		var rest string

		switch {
		case strings.HasPrefix(trimmed, "[ALIGNED]"):
			continue
		case strings.HasPrefix(trimmed, "[SCOPE_CREEP]"):
			sev = SeverityMedium
			cat = CategoryScopeCreep
			rest = strings.TrimPrefix(trimmed, "[SCOPE_CREEP]")
		case strings.HasPrefix(trimmed, "[MISSING]"):
			sev = SeverityCritical
			cat = CategoryIntent
			rest = strings.TrimPrefix(trimmed, "[MISSING]")
		case strings.HasPrefix(trimmed, "[DOCS_OUTDATED]"):
			sev = SeverityLow
			cat = CategoryStyle
			rest = strings.TrimPrefix(trimmed, "[DOCS_OUTDATED]")
		default:
			continue
		}

		title, detail := splitTitleDetail(rest)
		findings = append(findings, ReviewFinding{
			Source:   "factory",
			Severity: sev,
			Category: cat,
			Title:    title,
			Detail:   detail,
		})
	}
	return findings
}

// splitFindings splits text into blocks, one per finding marker.
func splitFindings(text string) []string {
	lines := strings.Split(text, "\n")
	var blocks []string
	var current strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && current.Len() > 0 {
			blocks = append(blocks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		blocks = append(blocks, current.String())
	}
	return blocks
}

// extractSeverityMarker returns the severity and remaining text after the marker.
func extractSeverityMarker(block string) (Severity, string) {
	trimmed := strings.TrimSpace(block)
	switch {
	case strings.HasPrefix(trimmed, "[CRITICAL]"):
		return SeverityCritical, strings.TrimPrefix(trimmed, "[CRITICAL]")
	case strings.HasPrefix(trimmed, "[MEDIUM]"):
		return SeverityMedium, strings.TrimPrefix(trimmed, "[MEDIUM]")
	case strings.HasPrefix(trimmed, "[LOW]"):
		return SeverityLow, strings.TrimPrefix(trimmed, "[LOW]")
	default:
		return "", ""
	}
}

// splitTitleDetail splits "Title — detail" into title and detail.
func splitTitleDetail(text string) (string, string) {
	text = strings.TrimSpace(text)
	for _, sep := range []string{" — ", " -- "} {
		if idx := strings.Index(text, sep); idx >= 0 {
			return strings.TrimSpace(text[:idx]), strings.TrimSpace(text[idx+len(sep):])
		}
	}
	return text, ""
}

// extractFileRef extracts a file path and line number from text.
func extractFileRef(text string) (string, int) {
	m := fileRefPattern.FindStringSubmatch(text)
	if m == nil {
		return "", 0
	}
	line, err := strconv.Atoi(m[2])
	if err != nil {
		return m[1], 0
	}
	return m[1], line
}
