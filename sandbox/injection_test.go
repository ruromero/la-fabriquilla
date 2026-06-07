package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type adversarialCase struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Input    string `json:"input"`
}

func loadAdversarialCorpus(t *testing.T) map[string][]adversarialCase {
	t.Helper()
	dir := filepath.Join("..", "testdata", "adversarial")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read adversarial directory: %v", err)
	}

	corpus := make(map[string][]adversarialCase)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read %s: %v", entry.Name(), err)
		}
		var cases []adversarialCase
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatalf("failed to parse %s: %v", entry.Name(), err)
		}
		category := strings.TrimSuffix(entry.Name(), ".json")
		corpus[category] = cases
	}
	return corpus
}

func TestAdversarial_SanitizeInput(t *testing.T) {
	corpus := loadAdversarialCorpus(t)

	for category, cases := range corpus {
		t.Run(category, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					result := SanitizeInput(tc.Input)

					// Zero-width characters must be stripped
					for _, r := range result {
						switch r {
						case 0x200B, 0x200C, 0x200D, 0xFEFF:
							t.Errorf("zero-width character U+%04X not stripped", r)
						}
					}

					// Bidi overrides must be stripped
					for _, r := range result {
						if r >= 0x202A && r <= 0x202E {
							t.Errorf("bidi override U+%04X not stripped", r)
						}
						if r >= 0x2066 && r <= 0x2069 {
							t.Errorf("bidi isolate U+%04X not stripped", r)
						}
					}

					// Tag characters must be stripped
					for _, r := range result {
						if r >= 0xE0000 && r <= 0xE007F {
							t.Errorf("tag character U+%04X not stripped", r)
						}
					}

					// No control characters except whitespace
					for _, r := range result {
						if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
							t.Errorf("control character U+%04X not stripped", r)
						}
					}
				})
			}
		})
	}
}

func TestAdversarial_RedactSecrets(t *testing.T) {
	corpus := loadAdversarialCorpus(t)
	patterns := GetSensitivePatterns()

	for category, cases := range corpus {
		t.Run(category, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					sanitized := SanitizeInput(tc.Input)
					redacted, events := RedactSecrets(sanitized)

					// For each pattern that matches the sanitized input,
					// verify the redacted output no longer contains the match.
					for _, sp := range patterns {
						matches := sp.Pattern.FindAllString(sanitized, -1)
						for _, match := range matches {
							if strings.Contains(redacted, match) {
								t.Errorf("pattern %q matched %q but was not redacted", sp.Name, match)
							}
						}
					}

					// If any redaction events occurred, verify markers are present.
					for _, ev := range events {
						marker := "[REDACTED:" + ev.Pattern + "]"
						if !strings.Contains(redacted, marker) {
							t.Errorf("redaction event for %q but marker %q not in output", ev.Pattern, marker)
						}
					}
				})
			}
		})
	}
}

func TestAdversarial_PromptDelimiters(t *testing.T) {
	corpus := loadAdversarialCorpus(t)

	// Simulate a simple prompt structure with adversarial input embedded
	const promptTemplate = "## System\n\nYou are a code reviewer.\n\n## Issue\n\n%s\n\n## Instructions\n\nReview the code."

	for category, cases := range corpus {
		t.Run(category, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					sanitized := SanitizeInput(tc.Input)
					redacted, _ := RedactSecrets(sanitized)

					// Build prompt with the adversarial input in the Issue section
					prompt := strings.Replace(promptTemplate, "%s", redacted, 1)

					// Verify the structural delimiters are intact
					if !strings.Contains(prompt, "## System\n") {
						t.Error("System section delimiter corrupted")
					}
					if !strings.Contains(prompt, "## Issue\n") {
						t.Error("Issue section delimiter corrupted")
					}
					if !strings.Contains(prompt, "## Instructions\n") {
						t.Error("Instructions section delimiter corrupted")
					}

					// Verify the fixed sections still have expected content
					if !strings.Contains(prompt, "You are a code reviewer.") {
						t.Error("System section content corrupted")
					}
					if !strings.Contains(prompt, "Review the code.") {
						t.Error("Instructions section content corrupted")
					}
				})
			}
		})
	}
}
