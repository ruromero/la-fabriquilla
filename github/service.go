package github

import "context"

// Service defines the GitHub operations used by the pipeline.
// Implementations: *Client (real API), *MemoryClient (test stub).
type Service interface {
	Owner() string
	Repo() string

	// Issues
	ListIssuesByLabel(ctx context.Context, label string) ([]Issue, error)
	ListLabels(ctx context.Context, issueNumber int) ([]Label, error)
	AddLabel(ctx context.Context, issueNumber int, label string) error
	RemoveLabel(ctx context.Context, issueNumber int, label string) error
	CreateComment(ctx context.Context, issueNumber int, body string) error
	ListComments(ctx context.Context, issueNumber int) ([]Comment, error)
	CreateIssue(ctx context.Context, title, body string, labels []string) (Issue, error)

	// Repo content
	FileExists(ctx context.Context, path string) (bool, error)
	GetFileContent(ctx context.Context, path string) (string, error)
	CheckReadiness(ctx context.Context) (ReadinessResult, error)
	CloneShallow(ctx context.Context) (string, func(), error)

	// Git operations
	GetBranchSHA(ctx context.Context, branch string) (string, error)
	CreateBranch(ctx context.Context, branchName, sha string) error
	CreateCommit(ctx context.Context, branch, message string, files []FileChange) (string, error)
	CreatePullRequest(ctx context.Context, title, body, head, base string) (PullRequest, error)

	// PR review
	ListPRReviewComments(ctx context.Context, prNumber int) ([]Comment, error)
	ListPRReviews(ctx context.Context, prNumber int) ([]PRReview, error)

	// Cleanup
	ClosePullRequest(ctx context.Context, prNumber int) error
	DeleteBranch(ctx context.Context, branch string) error
	CloseIssue(ctx context.Context, issueNumber int) error
}
