package testutil

import (
	"context"
	"testing"

	"github.com/ruromero/la-fabriquilla/github"
)

func TestMemoryClientImplementsService(t *testing.T) {
	var _ github.Service = (*MemoryClient)(nil)
}

func TestMemoryClientListIssuesByLabel(t *testing.T) {
	mc := NewMemoryClient("test-owner", "test-repo",
		WithIssue(github.Issue{Number: 1, Title: "test issue", Body: "body", Labels: []github.Label{{Name: "fabriquilla:ready"}}}),
	)
	issues, err := mc.ListIssuesByLabel(context.Background(), "fabriquilla:ready")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].Number != 1 {
		t.Errorf("issue number = %d, want 1", issues[0].Number)
	}
}

func TestMemoryClientLabelTransitions(t *testing.T) {
	mc := NewMemoryClient("test-owner", "test-repo",
		WithIssue(github.Issue{Number: 1, Title: "test", Labels: []github.Label{{Name: "fabriquilla:ready"}}}),
	)
	ctx := context.Background()

	if err := mc.AddLabel(ctx, 1, "fabriquilla:in-progress"); err != nil {
		t.Fatal(err)
	}
	if err := mc.RemoveLabel(ctx, 1, "fabriquilla:ready"); err != nil {
		t.Fatal(err)
	}

	issues, _ := mc.ListIssuesByLabel(ctx, "fabriquilla:ready")
	if len(issues) != 0 {
		t.Error("issue should no longer have fabriquilla:ready label")
	}
	issues, _ = mc.ListIssuesByLabel(ctx, "fabriquilla:in-progress")
	if len(issues) != 1 {
		t.Error("issue should have fabriquilla:in-progress label")
	}
}

func TestMemoryClientCreatePR(t *testing.T) {
	mc := NewMemoryClient("test-owner", "test-repo")
	ctx := context.Background()

	pr, err := mc.CreatePullRequest(ctx, "title", "body", "feat-branch", "main")
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 1 {
		t.Errorf("pr number = %d, want 1", pr.Number)
	}
	if len(mc.CreatedPRs) != 1 {
		t.Error("expected 1 created PR in history")
	}
}
