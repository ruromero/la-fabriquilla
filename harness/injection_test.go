package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruromero/la-fabriquilla/sandbox"
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

// escapeSectionDelimiters prevents untrusted input from injecting new
// sections by escaping leading "## " on each line with a zero-width space.
func escapeSectionDelimiters(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			lines[i] = "#​# " + line[3:]
		}
	}
	return strings.Join(lines, "\n")
}

// assemblePrompt simulates how the harness assembles a prompt with
// issue data embedded in a designated section. Untrusted inputs are
// escaped to prevent section delimiter injection.
func assemblePrompt(title, body string) string {
	var b strings.Builder
	b.WriteString("## Role\n\nYou are a code reviewer.\n\n")
	b.WriteString("## Repository Context\n\nA Go project with stdlib only.\n\n")
	b.WriteString(fmt.Sprintf("## Issue Title\n\n%s\n\n", escapeSectionDelimiters(title)))
	b.WriteString(fmt.Sprintf("## Issue Body\n\n%s\n\n", escapeSectionDelimiters(body)))
	b.WriteString("## Task\n\nReview the issue and provide a plan.\n")
	return b.String()
}

func TestAdversarial_StructuralIntegrity(t *testing.T) {
	corpus := loadAdversarialCorpus(t)

	expectedSections := []string{"Role", "Repository Context", "Issue Title", "Issue Body", "Task"}

	for category, cases := range corpus {
		t.Run(category, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					// Sanitize and redact the adversarial input
					sanitized := sandbox.SanitizeInput(tc.Input)
					redacted, _ := sandbox.RedactSecrets(sanitized)

					// Assemble prompt with adversarial input as both title and body
					prompt := assemblePrompt(redacted, redacted)

					// Parse sections using the harness parser
					sections := ParseSections(prompt)
					names := SectionNames(sections)

					// Verify all expected sections are present
					for _, expected := range expectedSections {
						found := false
						for _, name := range names {
							if name == expected {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("section %q missing from assembled prompt", expected)
						}
					}

					// Verify the injected content stays within its designated sections
					roleContent, ok := FindSection(sections, "Role")
					if !ok {
						t.Fatal("Role section not found")
					}
					if roleContent != "You are a code reviewer." {
						t.Errorf("Role section contaminated: got %q", roleContent)
					}

					repoContent, ok := FindSection(sections, "Repository Context")
					if !ok {
						t.Fatal("Repository Context section not found")
					}
					if repoContent != "A Go project with stdlib only." {
						t.Errorf("Repository Context section contaminated: got %q", repoContent)
					}

					taskContent, ok := FindSection(sections, "Task")
					if !ok {
						t.Fatal("Task section not found")
					}
					if taskContent != "Review the issue and provide a plan." {
						t.Errorf("Task section contaminated: got %q", taskContent)
					}

					// With delimiter escaping, adversarial inputs must not
					// introduce extra sections.
					if len(names) != len(expectedSections) {
						extra := []string{}
						for _, name := range names {
							found := false
							for _, exp := range expectedSections {
								if name == exp {
									found = true
									break
								}
							}
							if !found {
								extra = append(extra, name)
							}
						}
						if len(extra) > 0 {
							t.Errorf("adversarial input injected extra sections: %v", extra)
						}
					}
				})
			}
		})
	}
}

func TestAdversarial_SectionDelimiterPreservation(t *testing.T) {
	corpus := loadAdversarialCorpus(t)

	for category, cases := range corpus {
		t.Run(category, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					sanitized := sandbox.SanitizeInput(tc.Input)
					redacted, _ := sandbox.RedactSecrets(sanitized)

					prompt := assemblePrompt(redacted, redacted)

					// Verify the prompt starts with the expected structure
					if !strings.HasPrefix(prompt, "## Role\n") {
						t.Error("prompt does not start with Role section")
					}

					// Verify the Task section appears and ends the prompt
					if !strings.Contains(prompt, "## Task\n") {
						t.Error("Task section delimiter missing")
					}

					// Verify the prompt ends with the task content
					if !strings.HasSuffix(prompt, "Review the issue and provide a plan.\n") {
						t.Error("prompt does not end with expected task content")
					}
				})
			}
		})
	}
}
