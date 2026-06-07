package agents

import (
	"testing"

	"github.com/ruromero/la-fabriquilla/review"
)

func TestFilterDismissedFindings(t *testing.T) {
	findings := []review.ReviewFinding{
		{Title: "nil pointer deref", Severity: review.SeverityCritical},
		{Title: "missing error check", Severity: review.SeverityMedium},
		{Title: "old dismissed finding", Severity: review.SeverityCritical},
	}

	t.Run("filters previously dismissed", func(t *testing.T) {
		dismissed := []string{"old dismissed finding"}
		remaining, autoDismissed := filterDismissedFindings(findings, dismissed)
		if len(remaining) != 2 {
			t.Fatalf("remaining = %d, want 2", len(remaining))
		}
		if len(autoDismissed) != 1 {
			t.Fatalf("autoDismissed = %d, want 1", len(autoDismissed))
		}
		if autoDismissed[0].Finding.Title != "old dismissed finding" {
			t.Errorf("auto-dismissed title = %q", autoDismissed[0].Finding.Title)
		}
		if autoDismissed[0].Classification != review.ClassDismissed {
			t.Errorf("classification = %q, want dismissed", autoDismissed[0].Classification)
		}
	})

	t.Run("no dismissed titles passes all through", func(t *testing.T) {
		remaining, autoDismissed := filterDismissedFindings(findings, nil)
		if len(remaining) != 3 {
			t.Fatalf("remaining = %d, want 3", len(remaining))
		}
		if len(autoDismissed) != 0 {
			t.Fatalf("autoDismissed = %d, want 0", len(autoDismissed))
		}
	})

	t.Run("empty findings", func(t *testing.T) {
		remaining, autoDismissed := filterDismissedFindings(nil, []string{"something"})
		if len(remaining) != 0 {
			t.Fatalf("remaining = %d, want 0", len(remaining))
		}
		if len(autoDismissed) != 0 {
			t.Fatalf("autoDismissed = %d, want 0", len(autoDismissed))
		}
	})
}

func TestParseArbiterResponse(t *testing.T) {
	t.Run("valid JSON response", func(t *testing.T) {
		input := `{"findings":[{"finding":{"source":"factory","severity":"critical","category":"correctness","title":"nil pointer"},"classification":"fix_here","reason":"simple fix"}]}`
		result, err := parseArbiterResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Findings) != 1 {
			t.Fatalf("findings count = %d, want 1", len(result.Findings))
		}
		if result.Findings[0].Classification != review.ClassFixHere {
			t.Errorf("classification = %q, want fix_here", result.Findings[0].Classification)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, err := parseArbiterResponse("not json")
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("empty findings", func(t *testing.T) {
		result, err := parseArbiterResponse(`{"findings":[]}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Findings) != 0 {
			t.Fatalf("findings count = %d, want 0", len(result.Findings))
		}
	})
}
