package review

import (
	"context"
	"testing"
	"time"
)

func TestHumanAdapter_TriggerReview(t *testing.T) {
	adapter := &HumanAdapter{}
	mock := &mockPRCommentClient{}
	if err := adapter.TriggerReview(context.Background(), mock, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.created) != 0 {
		t.Errorf("TriggerReview should be no-op, but created %d comments", len(mock.created))
	}
}

func TestHumanAdapter_ReviewReady(t *testing.T) {
	adapter := &HumanAdapter{}

	t.Run("ready when human submits changes_requested", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviews: []PRReview{
				{ID: 1, State: "COMMENTED", User: "alice", SubmittedAt: time.Now()},
				{ID: 2, State: "CHANGES_REQUESTED", User: "bob", SubmittedAt: time.Now()},
			},
		}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ready {
			t.Error("expected ready=true when CHANGES_REQUESTED review exists")
		}
	})

	t.Run("not ready with only approved reviews", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviews: []PRReview{
				{ID: 1, State: "APPROVED", User: "alice", SubmittedAt: time.Now()},
			},
		}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Error("expected ready=false when no CHANGES_REQUESTED review")
		}
	})

	t.Run("not ready with no reviews", func(t *testing.T) {
		mock := &mockPRCommentClient{}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Error("expected ready=false with empty reviews")
		}
	})

	t.Run("ignores bot changes_requested", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviews: []PRReview{
				{ID: 1, State: "CHANGES_REQUESTED", User: "some-tool[bot]", SubmittedAt: time.Now()},
			},
		}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Error("expected ready=false when only bot submitted CHANGES_REQUESTED")
		}
	})
}

func TestHumanAdapter_ParseFindings(t *testing.T) {
	adapter := &HumanAdapter{}

	t.Run("parses human review comments", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 1, Body: "This has a security vulnerability in the auth handler", User: "alice", CreatedAt: time.Now()},
				{ID: 2, Body: "The test is missing for this edge case", User: "bob", CreatedAt: time.Now()},
			},
		}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 2 {
			t.Fatalf("got %d findings, want 2", len(findings))
		}

		if findings[0].Source != "human" {
			t.Errorf("source = %q, want %q", findings[0].Source, "human")
		}
		if findings[0].Category != CategorySecurity {
			t.Errorf("category = %q, want %q", findings[0].Category, CategorySecurity)
		}
		if findings[0].Severity != SeverityMedium {
			t.Errorf("severity = %q, want %q", findings[0].Severity, SeverityMedium)
		}
		if findings[1].Category != CategoryMissingTests {
			t.Errorf("category = %q, want %q", findings[1].Category, CategoryMissingTests)
		}
	})

	t.Run("skips bot comments", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 1, Body: "automated check passed", User: "ci-bot[bot]", CreatedAt: time.Now()},
				{ID: 2, Body: "fix this bug", User: "alice", CreatedAt: time.Now()},
			},
		}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		if findings[0].Category != CategoryCorrectness {
			t.Errorf("category = %q, want %q", findings[0].Category, CategoryCorrectness)
		}
	})

	t.Run("skips empty comments", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 1, Body: "   ", User: "alice", CreatedAt: time.Now()},
			},
		}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("got %d findings, want 0", len(findings))
		}
	})

	t.Run("no review comments", func(t *testing.T) {
		mock := &mockPRCommentClient{}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("got %d findings, want 0", len(findings))
		}
	})

	t.Run("truncates long titles", func(t *testing.T) {
		long := "This is a very long comment that goes on and on and on and on and should be truncated because it exceeds the maximum title length that we allow for findings"
		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 1, Body: long, User: "alice", CreatedAt: time.Now()},
			},
		}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		if len(findings[0].Title) > 120 {
			t.Errorf("title length = %d, want <= 120", len(findings[0].Title))
		}
	})
}

func TestParseHumanComment(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCat  Category
		wantSev  Severity
	}{
		{"security keyword", "This endpoint has an injection vulnerability", CategorySecurity, SeverityMedium},
		{"bug keyword", "This is a bug in the parser", CategoryCorrectness, SeverityMedium},
		{"error handling", "Missing error handling for nil pointer", CategoryErrorHandling, SeverityMedium},
		{"test keyword", "Need to add a test for this", CategoryMissingTests, SeverityMedium},
		{"performance keyword", "This loop is too slow", CategoryPerformance, SeverityMedium},
		{"default category", "Please refactor this function", CategoryCorrectness, SeverityMedium},
		{"multiline uses first line as title", "First line\nSecond line\nThird line", CategoryCorrectness, SeverityMedium},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := parseHumanComment(tt.body)
			if f.Category != tt.wantCat {
				t.Errorf("category = %q, want %q", f.Category, tt.wantCat)
			}
			if f.Severity != tt.wantSev {
				t.Errorf("severity = %q, want %q", f.Severity, tt.wantSev)
			}
			if f.Source != "human" {
				t.Errorf("source = %q, want %q", f.Source, "human")
			}
		})
	}

	t.Run("multiline title", func(t *testing.T) {
		f := parseHumanComment("First line\nSecond line")
		if f.Title != "First line" {
			t.Errorf("title = %q, want %q", f.Title, "First line")
		}
		if f.Detail != "First line\nSecond line" {
			t.Errorf("detail = %q, want full body", f.Detail)
		}
	})
}

func TestIsBot(t *testing.T) {
	if !isBot("dependabot[bot]") {
		t.Error("expected dependabot[bot] to be a bot")
	}
	if !isBot("qodo-code-review[bot]") {
		t.Error("expected qodo-code-review[bot] to be a bot")
	}
	if isBot("alice") {
		t.Error("expected alice to not be a bot")
	}
	if isBot("") {
		t.Error("expected empty string to not be a bot")
	}
}
