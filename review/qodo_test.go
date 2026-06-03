package review

import (
	"context"
	"testing"
	"time"
)

// mockPRCommentClient implements PRCommentClient for testing.
type mockPRCommentClient struct {
	comments       []PRComment
	reviewComments []PRComment
	created        []string
}

func (m *mockPRCommentClient) CreateComment(_ context.Context, _ int, body string) error {
	m.created = append(m.created, body)
	return nil
}

func (m *mockPRCommentClient) ListPRComments(_ context.Context, _ int) ([]PRComment, error) {
	return m.comments, nil
}

func (m *mockPRCommentClient) ListPRReviewComments(_ context.Context, _ int) ([]PRComment, error) {
	return m.reviewComments, nil
}

func TestQodoAdapter_TriggerReview(t *testing.T) {
	mock := &mockPRCommentClient{}
	adapter := &QodoAdapter{BotLogin: "qodo-code-review[bot]"}
	if err := adapter.TriggerReview(context.Background(), mock, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.created) != 1 {
		t.Fatalf("got %d comments, want 1", len(mock.created))
	}
	if mock.created[0] != "/agentic_review" {
		t.Errorf("comment = %q, want %q", mock.created[0], "/agentic_review")
	}
}

func TestQodoAdapter_ReviewReady(t *testing.T) {
	adapter := &QodoAdapter{BotLogin: "qodo-code-review[bot]"}

	t.Run("ready when bot summary present", func(t *testing.T) {
		mock := &mockPRCommentClient{
			comments: []PRComment{
				{ID: 1, Body: "some human comment", User: "developer"},
				{ID: 2, Body: "<h3>Code Review by Qodo</h3>\n<code>bugs</code>", User: "qodo-code-review[bot]", CreatedAt: time.Now()},
			},
		}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ready {
			t.Error("expected ready=true")
		}
	})

	t.Run("not ready when no bot comment", func(t *testing.T) {
		mock := &mockPRCommentClient{
			comments: []PRComment{
				{ID: 1, Body: "just a human comment", User: "developer"},
			},
		}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Error("expected ready=false")
		}
	})

	t.Run("not ready for walkthrough comment only", func(t *testing.T) {
		mock := &mockPRCommentClient{
			comments: []PRComment{
				{ID: 1, Body: "<h3>Review Summary by Qodo</h3>", User: "qodo-code-review[bot]"},
			},
		}
		ready, err := adapter.ReviewReady(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Error("expected ready=false — walkthrough != code review")
		}
	})
}

func TestQodoAdapter_ParseFindings(t *testing.T) {
	adapter := &QodoAdapter{BotLogin: "qodo-code-review[bot]"}

	t.Run("parses action required finding", func(t *testing.T) {
		body := "<img src=\"https://www.qodo.ai/wp-content/uploads/2026/01/action-required.png\" height=\"20\" alt=\"Action required\">\n\n1\\. Labels missing <b><i>htmlfor</i></b> and <b><i>id</i></b> <code>\xf0\x9f\x93\x98 Rule violation</code> <code>\xe2\x98\x91 Accessibility</code>\n\n<pre>\nIn ArtistsPage hero search bar, visible label elements are not associated\nwith their corresponding input elements.\n</pre>\n\n\n<details>\n<summary><strong>Agent Prompt</strong></summary>\n\n```\n## Fix Focus Areas\n- src/pages/ArtistsPage.tsx[351-442]\n```\n</details>"

		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 10, Body: body, User: "qodo-code-review[bot]"},
			},
		}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		f := findings[0]
		if f.Source != "qodo" {
			t.Errorf("source = %q, want %q", f.Source, "qodo")
		}
		if f.Severity != SeverityCritical {
			t.Errorf("severity = %q, want %q", f.Severity, SeverityCritical)
		}
		if f.Category != CategoryStyle {
			t.Errorf("category = %q, want %q", f.Category, CategoryStyle)
		}
		if f.Title != "Labels missing htmlfor and id" {
			t.Errorf("title = %q, want %q", f.Title, "Labels missing htmlfor and id")
		}
		if f.File != "src/pages/ArtistsPage.tsx" {
			t.Errorf("file = %q, want %q", f.File, "src/pages/ArtistsPage.tsx")
		}
		if f.Line != 351 {
			t.Errorf("line = %d, want 351", f.Line)
		}
	})

	t.Run("parses correctness bug", func(t *testing.T) {
		body := "<img src=\"https://www.qodo.ai/wp-content/uploads/2026/01/action-required.png\" height=\"20\" alt=\"Action required\">\n\n2\\. Location filter ignored <code>\xf0\x9f\x90\x9e Bug</code> <code>\xe2\x89\xa1 Correctness</code>\n\n<pre>\nArtistsPage builds the backend search parameter incorrectly.\n</pre>\n\n\n<details>\n<summary><strong>Agent Prompt</strong></summary>\n\n```\n## Fix Focus Areas\n- src/pages/ArtistsPage.tsx[254-270]\n- packages/api/src/repositories/artist.repository.ts[43-71]\n```\n</details>"

		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 20, Body: body, User: "qodo-code-review[bot]"},
			},
		}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1", len(findings))
		}
		f := findings[0]
		if f.Severity != SeverityCritical {
			t.Errorf("severity = %q, want %q", f.Severity, SeverityCritical)
		}
		if f.Category != CategoryCorrectness {
			t.Errorf("category = %q, want %q", f.Category, CategoryCorrectness)
		}
		if f.Title != "Location filter ignored" {
			t.Errorf("title = %q, want %q", f.Title, "Location filter ignored")
		}
		if f.File != "src/pages/ArtistsPage.tsx" {
			t.Errorf("file = %q, want %q", f.File, "src/pages/ArtistsPage.tsx")
		}
		if f.Line != 254 {
			t.Errorf("line = %d, want 254", f.Line)
		}
	})

	t.Run("skips non-bot comments", func(t *testing.T) {
		mock := &mockPRCommentClient{
			reviewComments: []PRComment{
				{ID: 30, Body: "looks good to me", User: "developer"},
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

	t.Run("empty review comments", func(t *testing.T) {
		mock := &mockPRCommentClient{}
		findings, err := adapter.ParseFindings(context.Background(), mock, 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("got %d findings, want 0", len(findings))
		}
	})
}
