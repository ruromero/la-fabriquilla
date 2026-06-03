package review

import (
	"context"
	"time"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

type Category string

const (
	CategoryCorrectness   Category = "correctness"
	CategorySecurity      Category = "security"
	CategoryIntent        Category = "intent"
	CategoryMissingTests  Category = "missing_tests"
	CategoryErrorHandling Category = "error_handling"
	CategoryPerformance   Category = "performance"
	CategoryStyle         Category = "style"
	CategoryScopeCreep    Category = "scope_creep"
)

type ReviewFinding struct {
	Source   string   `json:"source"`
	Severity Severity `json:"severity"`
	Category Category `json:"category"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
}

type PRComment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

type PRCommentClient interface {
	CreateComment(ctx context.Context, issueNumber int, body string) error
	// ListPRComments returns issue-level comments (GET /repos/{owner}/{repo}/issues/{number}/comments).
	// Qodo posts its summary comment here.
	ListPRComments(ctx context.Context, prNumber int) ([]PRComment, error)
	// ListPRReviewComments returns pull-request review comments attached to diff lines
	// (GET /repos/{owner}/{repo}/pulls/{number}/comments). Qodo posts inline findings here.
	ListPRReviewComments(ctx context.Context, prNumber int) ([]PRComment, error)
}
