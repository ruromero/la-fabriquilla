package github

import (
	"context"

	"github.com/ruromero/la-fabriquilla/review"
)

// ReviewClient wraps Client to satisfy review.PRCommentClient.
type ReviewClient struct {
	*Client
}

// ListPRComments returns issue-level comments (issues API).
func (rc *ReviewClient) ListPRComments(ctx context.Context, prNumber int) ([]review.PRComment, error) {
	comments, err := rc.Client.ListComments(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	return toPRComments(comments), nil
}

// ListPRReviewComments returns pull-request review comments attached to diff lines (pulls API).
func (rc *ReviewClient) ListPRReviewComments(ctx context.Context, prNumber int) ([]review.PRComment, error) {
	comments, err := rc.Client.ListPRReviewComments(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	return toPRComments(comments), nil
}

func (rc *ReviewClient) CreateComment(ctx context.Context, issueNumber int, body string) error {
	return rc.Client.CreateComment(ctx, issueNumber, body)
}

func toPRComments(comments []Comment) []review.PRComment {
	result := make([]review.PRComment, len(comments))
	for i, c := range comments {
		result[i] = review.PRComment{
			ID:        c.ID,
			Body:      c.Body,
			User:      c.User.Login,
			CreatedAt: c.CreatedAt,
		}
	}
	return result
}
