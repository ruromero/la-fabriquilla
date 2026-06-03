package review

import "context"

// ExternalReviewAdapter abstracts an external review tool.
// Lifecycle: TriggerReview → poll ReviewReady → ParseFindings.
type ExternalReviewAdapter interface {
	// TriggerReview asks the external tool to review a PR.
	TriggerReview(ctx context.Context, client PRCommentClient, prNumber int) error

	// ReviewReady checks whether the external tool has responded.
	ReviewReady(ctx context.Context, client PRCommentClient, prNumber int) (bool, error)

	// ParseFindings extracts structured findings from the tool's response.
	ParseFindings(ctx context.Context, client PRCommentClient, prNumber int) ([]ReviewFinding, error)
}
