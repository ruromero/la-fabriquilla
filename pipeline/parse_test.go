package pipeline

import (
	"strings"
	"testing"

	"github.com/ruromero/la-fabriquilla/review"
)

func TestParseCodeOutput(t *testing.T) {
	t.Run("single file", func(t *testing.T) {
		input := "FILE: main.go\n```go\npackage main\n```"
		files := ParseCodeOutput(input)
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if files[0].Path != "main.go" {
			t.Errorf("path = %q, want %q", files[0].Path, "main.go")
		}
		if files[0].Content != "package main" {
			t.Errorf("content = %q, want %q", files[0].Content, "package main")
		}
	})

	t.Run("multiple files", func(t *testing.T) {
		input := "FILE: a.go\n```go\npackage a\n```\nFILE: b.go\n```go\npackage b\n```"
		files := ParseCodeOutput(input)
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2", len(files))
		}
		if files[0].Path != "a.go" {
			t.Errorf("files[0].path = %q, want %q", files[0].Path, "a.go")
		}
		if files[1].Path != "b.go" {
			t.Errorf("files[1].path = %q, want %q", files[1].Path, "b.go")
		}
	})

	t.Run("no files", func(t *testing.T) {
		files := ParseCodeOutput("just some text with no file markers")
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		files := ParseCodeOutput("")
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})
}

func TestParseStructuredCodeOutput(t *testing.T) {
	t.Run("valid JSON with two files", func(t *testing.T) {
		input := `{"files":[{"path":"main.go","language":"go","content":"package main"},{"path":"util.go","language":"go","content":"package util"}]}`
		files, err := ParseStructuredCodeOutput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2", len(files))
		}
		if files[0].Path != "main.go" {
			t.Errorf("files[0].path = %q, want %q", files[0].Path, "main.go")
		}
		if files[0].Content != "package main" {
			t.Errorf("files[0].content = %q, want %q", files[0].Content, "package main")
		}
		if files[1].Path != "util.go" {
			t.Errorf("files[1].path = %q, want %q", files[1].Path, "util.go")
		}
		if files[1].Content != "package util" {
			t.Errorf("files[1].content = %q, want %q", files[1].Content, "package util")
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := ParseStructuredCodeOutput("not json at all")
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("empty files array returns empty slice", func(t *testing.T) {
		input := `{"files":[]}`
		files, err := ParseStructuredCodeOutput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})

	t.Run("missing files field returns empty slice", func(t *testing.T) {
		input := `{}`
		files, err := ParseStructuredCodeOutput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Fatalf("got %d files, want 0", len(files))
		}
	})

	t.Run("language field is optional", func(t *testing.T) {
		input := `{"files":[{"path":"readme.txt","content":"hello world"}]}`
		files, err := ParseStructuredCodeOutput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if files[0].Path != "readme.txt" {
			t.Errorf("path = %q, want %q", files[0].Path, "readme.txt")
		}
		if files[0].Content != "hello world" {
			t.Errorf("content = %q, want %q", files[0].Content, "hello world")
		}
	})

	t.Run("content with newlines and special chars", func(t *testing.T) {
		input := `{"files":[{"path":"main.go","content":"package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"}]}`
		files, err := ParseStructuredCodeOutput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		want := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
		if files[0].Content != want {
			t.Errorf("content = %q, want %q", files[0].Content, want)
		}
	})
}

func TestReviewNeedsIteration(t *testing.T) {
	t.Run("critical finding", func(t *testing.T) {
		findings := []review.ReviewFinding{{Severity: review.SeverityCritical}}
		if !ReviewNeedsIteration(findings) {
			t.Error("expected true for CRITICAL finding")
		}
	})

	t.Run("medium finding", func(t *testing.T) {
		findings := []review.ReviewFinding{{Severity: review.SeverityMedium}}
		if !ReviewNeedsIteration(findings) {
			t.Error("expected true for MEDIUM finding")
		}
	})

	t.Run("low finding only", func(t *testing.T) {
		findings := []review.ReviewFinding{{Severity: review.SeverityLow}}
		if ReviewNeedsIteration(findings) {
			t.Error("expected false for LOW-only findings")
		}
	})

	t.Run("empty findings", func(t *testing.T) {
		if ReviewNeedsIteration(nil) {
			t.Error("expected false for nil findings")
		}
	})
}

func TestFormatReviewFeedback(t *testing.T) {
	findings := []review.ReviewFinding{
		{Severity: review.SeverityCritical, Title: "Bug found", Detail: "nil pointer", File: "main.go", Line: 42},
		{Severity: review.SeverityLow, Title: "Style issue"},
	}
	result := FormatReviewFeedback(findings)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	for _, want := range []string{"[CRITICAL]", "Bug found", "nil pointer", "main.go:42", "[LOW]", "Style issue"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q", want)
		}
	}

	t.Run("empty findings", func(t *testing.T) {
		result := FormatReviewFeedback(nil)
		if !strings.Contains(result, "[PASS]") {
			t.Errorf("expected PASS marker for empty findings, got %q", result)
		}
	})
}

func TestArbiterNeedsIteration(t *testing.T) {
	t.Run("fix_here triggers iteration", func(t *testing.T) {
		findings := []review.ArbiterFinding{
			{Classification: review.ClassFixHere},
		}
		if !ArbiterNeedsIteration(findings) {
			t.Error("expected true for fix_here")
		}
	})

	t.Run("subtask triggers iteration", func(t *testing.T) {
		findings := []review.ArbiterFinding{
			{Classification: review.ClassSubtask},
		}
		if !ArbiterNeedsIteration(findings) {
			t.Error("expected true for subtask")
		}
	})

	t.Run("root_cause only does not trigger", func(t *testing.T) {
		findings := []review.ArbiterFinding{
			{Classification: review.ClassRootCause},
		}
		if ArbiterNeedsIteration(findings) {
			t.Error("expected false for root_cause only")
		}
	})

	t.Run("dismissed only does not trigger", func(t *testing.T) {
		findings := []review.ArbiterFinding{
			{Classification: review.ClassDismissed},
		}
		if ArbiterNeedsIteration(findings) {
			t.Error("expected false for dismissed only")
		}
	})

	t.Run("mixed with fix_here triggers", func(t *testing.T) {
		findings := []review.ArbiterFinding{
			{Classification: review.ClassDismissed},
			{Classification: review.ClassRootCause},
			{Classification: review.ClassFixHere},
		}
		if !ArbiterNeedsIteration(findings) {
			t.Error("expected true when fix_here is present")
		}
	})

	t.Run("nil findings", func(t *testing.T) {
		if ArbiterNeedsIteration(nil) {
			t.Error("expected false for nil findings")
		}
	})
}

func TestEffectiveFindings(t *testing.T) {
	raw := &ReviewState{
		Findings: []review.ReviewFinding{
			{Source: "factory", Severity: review.SeverityCritical, Title: "real bug", File: "main.go"},
			{Source: "factory", Severity: review.SeverityLow, Title: "style nit", File: "util.go"},
			{Source: "qodo", Severity: review.SeverityMedium, Title: "missing test", File: "handler.go"},
		},
	}

	t.Run("nil arbiter returns raw findings", func(t *testing.T) {
		result := EffectiveFindings(nil, raw)
		if len(result) != 3 {
			t.Fatalf("got %d findings, want 3", len(result))
		}
	})

	t.Run("empty arbiter findings returns raw", func(t *testing.T) {
		arbiter := &ArbiterState{Findings: nil}
		result := EffectiveFindings(arbiter, raw)
		if len(result) != 3 {
			t.Fatalf("got %d findings, want 3", len(result))
		}
	})

	t.Run("filters to fix_here and subtask only", func(t *testing.T) {
		arbiter := &ArbiterState{
			Findings: []review.ArbiterFinding{
				{Finding: raw.Findings[0], Classification: review.ClassFixHere, Reason: "fix it"},
				{Finding: raw.Findings[1], Classification: review.ClassDismissed, Reason: "not important"},
				{Finding: raw.Findings[2], Classification: review.ClassSubtask, Reason: "needs work"},
			},
		}
		result := EffectiveFindings(arbiter, raw)
		if len(result) != 2 {
			t.Fatalf("got %d findings, want 2", len(result))
		}
		if result[0].Title != "real bug" {
			t.Errorf("result[0].Title = %q, want %q", result[0].Title, "real bug")
		}
		if result[1].Title != "missing test" {
			t.Errorf("result[1].Title = %q, want %q", result[1].Title, "missing test")
		}
	})

	t.Run("all dismissed returns empty", func(t *testing.T) {
		arbiter := &ArbiterState{
			Findings: []review.ArbiterFinding{
				{Finding: raw.Findings[0], Classification: review.ClassDismissed, Reason: "false positive"},
				{Finding: raw.Findings[1], Classification: review.ClassRootCause, Reason: "systemic", ProposedTitle: "Fix it"},
			},
		}
		result := EffectiveFindings(arbiter, raw)
		if len(result) != 0 {
			t.Fatalf("got %d findings, want 0", len(result))
		}
	})

	t.Run("nil raw and nil arbiter returns nil", func(t *testing.T) {
		result := EffectiveFindings(nil, nil)
		if result != nil {
			t.Fatalf("got %v, want nil", result)
		}
	})
}
