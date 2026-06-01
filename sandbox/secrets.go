package sandbox

import (
	"regexp"
	"strings"
)

// SensitivePattern pairs a human-readable name with a compiled regex
// for detecting credentials, secrets, and internal infrastructure details.
type SensitivePattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// RedactionEvent records redactions of a single pattern for audit logging.
type RedactionEvent struct {
	Pattern   string
	Count     int
	FirstLine int
}

// sensitivePatterns is the shared list of secret-detection patterns,
// used by both RedactSecrets (input path) and pipeline.ValidateContents (output path).
var sensitivePatterns = []SensitivePattern{
	{"private key", regexp.MustCompile(`-----BEGIN\s+(RSA|EC|DSA|OPENSSH|PGP)?\s*PRIVATE KEY-----`)},
	{"API key assignment", regexp.MustCompile(`(?i)(api[_-]?key|apikey|secret[_-]?key|access[_-]?key)\s*[:=]\s*["']?[A-Za-z0-9/+=_-]{16,}`)},
	{"password assignment", regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*["'][^"']{4,}["']`)},
	{"bearer token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{20,}`)},
	{"connection string", regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqp)://[^\s"'` + "`" + `]+`)},
	{"IPv4 address", regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`)},
	{"hostname pattern", regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.(?:internal|local|lan|home|corp|intranet)\b`)},
	{"AWS access key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"GitHub token", regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{"generic secret env", regexp.MustCompile(`(?i)(GITHUB_TOKEN|GEMINI_API_KEY|PLANNER_API_KEY|DEEPSEEK_API_KEY|INFERENCE_API_KEY)\s*=\s*[^\s$]{8,}`)},
}

// pemBlock matches an entire PEM private key block (header through footer).
var pemBlock = regexp.MustCompile(`(?s)-----BEGIN\s+(?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----.*?-----END\s+(?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`)

// GetSensitivePatterns returns a copy of the shared pattern list.
func GetSensitivePatterns() []SensitivePattern {
	out := make([]SensitivePattern, len(sensitivePatterns))
	copy(out, sensitivePatterns)
	return out
}

// RedactSecrets scans text for credentials and replaces matches with
// [REDACTED:<pattern>]. Returns the redacted text and a summary of
// redaction events (one per pattern, with count) for audit logging.
func RedactSecrets(text string) (string, []RedactionEvent) {
	type tracker struct {
		count     int
		firstLine int
	}
	seen := make(map[string]*tracker)
	var order []string

	record := func(name string, line int) {
		t, ok := seen[name]
		if !ok {
			t = &tracker{firstLine: line}
			seen[name] = t
			order = append(order, name)
		}
		t.count++
	}

	// Multi-line pass: redact full PEM private key blocks before line-by-line.
	text = pemBlock.ReplaceAllStringFunc(text, func(match string) string {
		line := 1 + strings.Count(text[:strings.Index(text, match)], "\n")
		record("private key", line)
		return "[REDACTED:private key]"
	})

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		for _, sp := range sensitivePatterns {
			if sp.Name == "private key" {
				continue
			}
			n := len(sp.Pattern.FindAllStringIndex(line, -1))
			if n > 0 {
				line = sp.Pattern.ReplaceAllString(line, "[REDACTED:"+sp.Name+"]")
				for range n {
					record(sp.Name, i+1)
				}
			}
		}
		lines[i] = line
	}

	events := make([]RedactionEvent, 0, len(order))
	for _, name := range order {
		t := seen[name]
		events = append(events, RedactionEvent{
			Pattern:   name,
			Count:     t.count,
			FirstLine: t.firstLine,
		})
	}
	return strings.Join(lines, "\n"), events
}
