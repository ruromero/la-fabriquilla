package review

import (
	"context"
	"strings"
)

// HumanAdapter implements ExternalReviewAdapter for human PR reviews.
// Human reviews happen organically — there is nothing to trigger.
// ReviewReady detects CHANGES_REQUESTED review submissions.
// ParseFindings converts human review comments into structured findings.
type HumanAdapter struct{}

// TriggerReview is a no-op for human reviews.
func (h *HumanAdapter) TriggerReview(_ context.Context, _ PRCommentClient, _ int) error {
	return nil
}

// ReviewReady returns true if any human (non-bot) has submitted a
// CHANGES_REQUESTED review on the PR.
func (h *HumanAdapter) ReviewReady(ctx context.Context, client PRCommentClient, prNumber int) (bool, error) {
	reviews, err := client.ListPRReviews(ctx, prNumber)
	if err != nil {
		return false, err
	}
	for _, r := range reviews {
		if r.State == "CHANGES_REQUESTED" && !isBot(r.User) {
			return true, nil
		}
	}
	return false, nil
}

// ParseFindings reads all non-bot PR review comments and converts them
// into structured ReviewFinding values.
func (h *HumanAdapter) ParseFindings(ctx context.Context, client PRCommentClient, prNumber int) ([]ReviewFinding, error) {
	comments, err := client.ListPRReviewComments(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	var findings []ReviewFinding
	for _, c := range comments {
		if isBot(c.User) || strings.TrimSpace(c.Body) == "" {
			continue
		}
		findings = append(findings, parseHumanComment(c.Body))
	}
	return findings, nil
}

// isBot returns true if the user login looks like a GitHub bot account.
func isBot(login string) bool {
	return strings.HasSuffix(login, "[bot]")
}

// humanCategoryKeywords maps keywords to categories for human comment classification.
var humanCategoryKeywords = []struct {
	keyword  string
	category Category
}{
	{"security", CategorySecurity},
	{"vulnerability", CategorySecurity},
	{"xss", CategorySecurity},
	{"injection", CategorySecurity},
	{"bug", CategoryCorrectness},
	{"incorrect", CategoryCorrectness},
	{"wrong", CategoryCorrectness},
	{"error", CategoryErrorHandling},
	{"panic", CategoryErrorHandling},
	{"crash", CategoryErrorHandling},
	{"test", CategoryMissingTests},
	{"performance", CategoryPerformance},
	{"slow", CategoryPerformance},
}

// parseHumanComment converts a human review comment body into a ReviewFinding.
func parseHumanComment(body string) ReviewFinding {
	lower := strings.ToLower(body)
	cat := CategoryCorrectness
	for _, kw := range humanCategoryKeywords {
		if strings.Contains(lower, kw.keyword) {
			cat = kw.category
			break
		}
	}

	title := body
	if idx := strings.IndexByte(title, '\n'); idx >= 0 {
		title = title[:idx]
	}
	title = strings.TrimSpace(title)
	if title == "" {
		trimmed := strings.TrimSpace(body)
		if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
			title = trimmed[:idx]
		} else {
			title = trimmed
		}
	}
	if len(title) > 120 {
		title = title[:117] + "..."
	}

	return ReviewFinding{
		Source:   "human",
		Severity: SeverityMedium,
		Category: cat,
		Title:    title,
		Detail:   strings.TrimSpace(body),
	}
}
