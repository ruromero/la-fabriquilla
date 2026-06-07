package review

import (
	"encoding/json"
	"testing"
)

func TestClassificationConstants(t *testing.T) {
	for _, tt := range []struct {
		c    Classification
		want string
	}{
		{ClassFixHere, "fix_here"},
		{ClassSubtask, "subtask"},
		{ClassRootCause, "root_cause"},
		{ClassDismissed, "dismissed"},
	} {
		if string(tt.c) != tt.want {
			t.Errorf("Classification %q != %q", tt.c, tt.want)
		}
	}
}

func TestArbiterResultRoundTrip(t *testing.T) {
	original := ArbiterResult{
		Findings: []ArbiterFinding{
			{
				Finding:        ReviewFinding{Source: "factory", Severity: SeverityCritical, Category: CategoryCorrectness, Title: "nil pointer"},
				Classification: ClassFixHere,
				Reason:         "simple fix in iterator scope",
			},
			{
				Finding:        ReviewFinding{Source: "qodo", Severity: SeverityMedium, Category: CategorySecurity, Title: "SQL injection"},
				Classification: ClassRootCause,
				Reason:         "systemic input validation gap",
				ProposedTitle:  "Add input validation to all SQL queries",
			},
			{
				Finding:        ReviewFinding{Source: "factory", Severity: SeverityLow, Category: CategoryStyle, Title: "naming convention"},
				Classification: ClassDismissed,
				Reason:         "project uses this convention per CONVENTIONS.md",
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded ArbiterResult
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(loaded.Findings) != 3 {
		t.Fatalf("findings count = %d, want 3", len(loaded.Findings))
	}
	if loaded.Findings[0].Classification != ClassFixHere {
		t.Errorf("findings[0].classification = %q, want %q", loaded.Findings[0].Classification, ClassFixHere)
	}
	if loaded.Findings[1].ProposedTitle != "Add input validation to all SQL queries" {
		t.Errorf("findings[1].proposed_title = %q", loaded.Findings[1].ProposedTitle)
	}
	if loaded.Findings[2].Classification != ClassDismissed {
		t.Errorf("findings[2].classification = %q, want %q", loaded.Findings[2].Classification, ClassDismissed)
	}
}
