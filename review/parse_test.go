package review

import "testing"

func TestParseReviewFindings(t *testing.T) {
	t.Run("mixed severities with file refs", func(t *testing.T) {
		input := "[CRITICAL] Nil pointer in handleWebhook — event.Payload is not checked\nbefore dereference, cmd/dispatcher/main.go:142\n[MEDIUM] Missing cleanup on context cancel — goroutine leaks if context\nis cancelled mid-iteration, agents/iterator.go:88-95\n[PASS] No issues found."
		findings := ParseReviewFindings(input, CategoryCorrectness)
		if len(findings) != 2 {
			t.Fatalf("got %d findings, want 2", len(findings))
		}
		f := findings[0]
		if f.Source != "factory" {
			t.Errorf("source = %q, want %q", f.Source, "factory")
		}
		if f.Severity != SeverityCritical {
			t.Errorf("severity = %q, want %q", f.Severity, SeverityCritical)
		}
		if f.Category != CategoryCorrectness {
			t.Errorf("category = %q, want %q", f.Category, CategoryCorrectness)
		}
		if f.Title != "Nil pointer in handleWebhook" {
			t.Errorf("title = %q, want %q", f.Title, "Nil pointer in handleWebhook")
		}
		if f.File != "cmd/dispatcher/main.go" {
			t.Errorf("file = %q, want %q", f.File, "cmd/dispatcher/main.go")
		}
		if f.Line != 142 {
			t.Errorf("line = %d, want 142", f.Line)
		}
		f2 := findings[1]
		if f2.Severity != SeverityMedium {
			t.Errorf("severity = %q, want %q", f2.Severity, SeverityMedium)
		}
		if f2.File != "agents/iterator.go" {
			t.Errorf("file = %q, want %q", f2.File, "agents/iterator.go")
		}
		if f2.Line != 88 {
			t.Errorf("line = %d, want 88", f2.Line)
		}
	})

	t.Run("pass only", func(t *testing.T) {
		findings := ParseReviewFindings("[PASS] No issues found.", CategorySecurity)
		if len(findings) != 0 {
			t.Fatalf("got %d findings, want 0", len(findings))
		}
	})

	t.Run("low finding no file ref", func(t *testing.T) {
		input := "[LOW] Minor naming inconsistency — use camelCase for local vars"
		findings := ParseReviewFindings(input, CategorySecurity)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		if findings[0].Severity != SeverityLow {
			t.Errorf("severity = %q, want %q", findings[0].Severity, SeverityLow)
		}
		if findings[0].File != "" {
			t.Errorf("file = %q, want empty", findings[0].File)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		findings := ParseReviewFindings("", CategoryCorrectness)
		if len(findings) != 0 {
			t.Fatalf("got %d findings, want 0", len(findings))
		}
	})
}

func TestParseIntentFindings(t *testing.T) {
	t.Run("aligned produces no findings", func(t *testing.T) {
		findings := ParseIntentFindings("[ALIGNED] — PR matches the intent")
		if len(findings) != 0 {
			t.Fatalf("got %d findings, want 0", len(findings))
		}
	})

	t.Run("scope creep", func(t *testing.T) {
		input := "[SCOPE_CREEP] Added logging framework — not in plan"
		findings := ParseIntentFindings(input)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		f := findings[0]
		if f.Category != CategoryScopeCreep {
			t.Errorf("category = %q, want %q", f.Category, CategoryScopeCreep)
		}
		if f.Severity != SeverityMedium {
			t.Errorf("severity = %q, want %q", f.Severity, SeverityMedium)
		}
		if f.Title != "Added logging framework" {
			t.Errorf("title = %q, want %q", f.Title, "Added logging framework")
		}
	})

	t.Run("missing item", func(t *testing.T) {
		input := "[MISSING] Unit tests for handler — planned but not implemented"
		findings := ParseIntentFindings(input)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		if findings[0].Category != CategoryIntent {
			t.Errorf("category = %q, want %q", findings[0].Category, CategoryIntent)
		}
		if findings[0].Severity != SeverityCritical {
			t.Errorf("severity = %q, want %q", findings[0].Severity, SeverityCritical)
		}
	})

	t.Run("docs outdated", func(t *testing.T) {
		input := "[DOCS_OUTDATED] README still references old API — needs update"
		findings := ParseIntentFindings(input)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		if findings[0].Category != CategoryStyle {
			t.Errorf("category = %q, want %q", findings[0].Category, CategoryStyle)
		}
		if findings[0].Severity != SeverityLow {
			t.Errorf("severity = %q, want %q", findings[0].Severity, SeverityLow)
		}
	})

	t.Run("mixed intent markers", func(t *testing.T) {
		input := "[SCOPE_CREEP] Extra dependency — not planned\n[MISSING] Error codes — planned but absent\n[ALIGNED] — the rest matches"
		findings := ParseIntentFindings(input)
		if len(findings) != 2 {
			t.Fatalf("got %d findings, want 2", len(findings))
		}
	})
}
