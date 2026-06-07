package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruromero/la-fabriquilla/inference"
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

func fakeArbiterServer(t *testing.T, respond func(findings []review.ReviewFinding) review.ArbiterResult) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req inference.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		userMsg := req.Messages[len(req.Messages)-1].Content
		_ = userMsg

		var findings []review.ReviewFinding
		json.Unmarshal([]byte(extractJSONArray(userMsg)), &findings)

		result := respond(findings)
		data, _ := json.Marshal(result)

		resp := inference.ChatResponse{
			Choices: []inference.Choice{{
				Message:      inference.Message{Role: "assistant", Content: string(data)},
				FinishReason: "stop",
			}},
			Usage: inference.Usage{PromptTokens: 100, CompletionTokens: 50},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func extractJSONArray(s string) string {
	start := -1
	for i, c := range s {
		if c == '[' {
			start = i
			break
		}
	}
	if start == -1 {
		return "[]"
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return "[]"
}

func TestArbitrate_ClassifiesPlantedBug(t *testing.T) {
	srv := fakeArbiterServer(t, func(findings []review.ReviewFinding) review.ArbiterResult {
		var classified []review.ArbiterFinding
		for _, f := range findings {
			classified = append(classified, review.ArbiterFinding{
				Finding:        f,
				Classification: review.ClassFixHere,
				Reason:         "clear bug that iterator can fix",
			})
		}
		return review.ArbiterResult{Findings: classified}
	})
	defer srv.Close()

	cl := inference.NewClient(srv.URL)
	out, err := Arbitrate(context.Background(), cl, "test-model",
		[]review.ReviewFinding{
			{Source: "factory", Severity: review.SeverityCritical, Category: review.CategoryCorrectness, Title: "nil pointer dereference in handler"},
		},
		"", "", "", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(out.Result.Findings))
	}
	if out.Result.Findings[0].Classification != review.ClassFixHere {
		t.Errorf("classification = %q, want fix_here", out.Result.Findings[0].Classification)
	}
}

func TestArbitrate_ClassifiesScopeCreepAsRootCause(t *testing.T) {
	srv := fakeArbiterServer(t, func(findings []review.ReviewFinding) review.ArbiterResult {
		var classified []review.ArbiterFinding
		for _, f := range findings {
			classified = append(classified, review.ArbiterFinding{
				Finding:        f,
				Classification: review.ClassRootCause,
				Reason:         "cross-cutting concern outside PR scope",
				ProposedTitle:  fmt.Sprintf("Address: %s", f.Title),
			})
		}
		return review.ArbiterResult{Findings: classified}
	})
	defer srv.Close()

	cl := inference.NewClient(srv.URL)
	out, err := Arbitrate(context.Background(), cl, "test-model",
		[]review.ReviewFinding{
			{Source: "factory", Severity: review.SeverityMedium, Category: review.CategoryScopeCreep, Title: "refactor all error handling to use wrapped errors"},
		},
		"", "", "", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(out.Result.Findings))
	}
	f := out.Result.Findings[0]
	if f.Classification != review.ClassRootCause {
		t.Errorf("classification = %q, want root_cause", f.Classification)
	}
	if f.ProposedTitle == "" {
		t.Error("root_cause finding should have proposed_title")
	}
}

func TestArbitrate_DismissesConventionConflict(t *testing.T) {
	srv := fakeArbiterServer(t, func(findings []review.ReviewFinding) review.ArbiterResult {
		var classified []review.ArbiterFinding
		for _, f := range findings {
			classified = append(classified, review.ArbiterFinding{
				Finding:        f,
				Classification: review.ClassDismissed,
				Reason:         "project conventions explicitly allow this pattern",
			})
		}
		return review.ArbiterResult{Findings: classified}
	})
	defer srv.Close()

	cl := inference.NewClient(srv.URL)
	out, err := Arbitrate(context.Background(), cl, "test-model",
		[]review.ReviewFinding{
			{Source: "factory", Severity: review.SeverityLow, Category: review.CategoryStyle, Title: "use camelCase instead of snake_case"},
		},
		"Use snake_case for all local variables", "", "", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(out.Result.Findings))
	}
	if out.Result.Findings[0].Classification != review.ClassDismissed {
		t.Errorf("classification = %q, want dismissed", out.Result.Findings[0].Classification)
	}
}

func TestArbitrate_MixedClassifications(t *testing.T) {
	srv := fakeArbiterServer(t, func(findings []review.ReviewFinding) review.ArbiterResult {
		var classified []review.ArbiterFinding
		for _, f := range findings {
			var c review.Classification
			switch f.Severity {
			case review.SeverityCritical:
				c = review.ClassFixHere
			case review.SeverityMedium:
				c = review.ClassSubtask
			default:
				c = review.ClassDismissed
			}
			classified = append(classified, review.ArbiterFinding{
				Finding:        f,
				Classification: c,
				Reason:         "classified by severity",
			})
		}
		return review.ArbiterResult{Findings: classified}
	})
	defer srv.Close()

	cl := inference.NewClient(srv.URL)
	out, err := Arbitrate(context.Background(), cl, "test-model",
		[]review.ReviewFinding{
			{Source: "factory", Severity: review.SeverityCritical, Category: review.CategoryCorrectness, Title: "nil pointer"},
			{Source: "qodo", Severity: review.SeverityMedium, Category: review.CategorySecurity, Title: "missing input validation"},
			{Source: "factory", Severity: review.SeverityLow, Category: review.CategoryStyle, Title: "naming convention"},
		},
		"", "", "", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Result.Findings) != 3 {
		t.Fatalf("findings = %d, want 3", len(out.Result.Findings))
	}

	counts := map[review.Classification]int{}
	for _, f := range out.Result.Findings {
		counts[f.Classification]++
	}
	if counts[review.ClassFixHere] != 1 {
		t.Errorf("fix_here count = %d, want 1", counts[review.ClassFixHere])
	}
	if counts[review.ClassSubtask] != 1 {
		t.Errorf("subtask count = %d, want 1", counts[review.ClassSubtask])
	}
	if counts[review.ClassDismissed] != 1 {
		t.Errorf("dismissed count = %d, want 1", counts[review.ClassDismissed])
	}
}

func TestArbitrate_DeadlockPrevention_MergesAutoDismissed(t *testing.T) {
	srv := fakeArbiterServer(t, func(findings []review.ReviewFinding) review.ArbiterResult {
		var classified []review.ArbiterFinding
		for _, f := range findings {
			classified = append(classified, review.ArbiterFinding{
				Finding:        f,
				Classification: review.ClassFixHere,
				Reason:         "needs fix",
			})
		}
		return review.ArbiterResult{Findings: classified}
	})
	defer srv.Close()

	cl := inference.NewClient(srv.URL)
	out, err := Arbitrate(context.Background(), cl, "test-model",
		[]review.ReviewFinding{
			{Source: "factory", Severity: review.SeverityCritical, Category: review.CategoryCorrectness, Title: "new bug"},
			{Source: "factory", Severity: review.SeverityCritical, Category: review.CategoryCorrectness, Title: "previously dismissed bug"},
		},
		"", "", "", []string{"previously dismissed bug"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Result.Findings) != 2 {
		t.Fatalf("findings = %d, want 2 (1 from LLM + 1 auto-dismissed)", len(out.Result.Findings))
	}

	var fixHere, dismissed int
	for _, f := range out.Result.Findings {
		switch f.Classification {
		case review.ClassFixHere:
			fixHere++
		case review.ClassDismissed:
			dismissed++
			if f.Finding.Title != "previously dismissed bug" {
				t.Errorf("auto-dismissed title = %q, want %q", f.Finding.Title, "previously dismissed bug")
			}
		}
	}
	if fixHere != 1 {
		t.Errorf("fix_here = %d, want 1", fixHere)
	}
	if dismissed != 1 {
		t.Errorf("dismissed = %d, want 1", dismissed)
	}
}
