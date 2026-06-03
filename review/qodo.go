package review

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultQodoBotLogin = "qodo-code-review[bot]"

// QodoAdapter triggers and parses Qodo code reviews.
type QodoAdapter struct {
	BotLogin    string
	TriggeredAt time.Time
}

func (q *QodoAdapter) botLogin() string {
	if q.BotLogin != "" {
		return q.BotLogin
	}
	return defaultQodoBotLogin
}

// TriggerReview posts /agentic_review as an issue comment on the PR.
func (q *QodoAdapter) TriggerReview(ctx context.Context, client PRCommentClient, prNumber int) error {
	q.TriggeredAt = time.Now()
	return client.CreateComment(ctx, prNumber, "/agentic_review")
}

// ReviewReady checks whether Qodo has posted its code review summary comment
// after the most recent trigger.
func (q *QodoAdapter) ReviewReady(ctx context.Context, client PRCommentClient, prNumber int) (bool, error) {
	comments, err := client.ListPRComments(ctx, prNumber)
	if err != nil {
		return false, err
	}
	bot := q.botLogin()
	for _, c := range comments {
		if c.User == bot && strings.Contains(c.Body, "Code Review by Qodo") && !c.CreatedAt.Before(q.TriggeredAt) {
			return true, nil
		}
	}
	return false, nil
}

// ParseFindings reads inline review comments from Qodo posted after the most
// recent trigger and extracts structured findings.
func (q *QodoAdapter) ParseFindings(ctx context.Context, client PRCommentClient, prNumber int) ([]ReviewFinding, error) {
	comments, err := client.ListPRReviewComments(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	bot := q.botLogin()
	var findings []ReviewFinding
	for _, c := range comments {
		if c.User != bot || c.CreatedAt.Before(q.TriggeredAt) {
			continue
		}
		f := parseQodoComment(c.Body)
		if f.Title == "" {
			slog.Warn("qodo comment produced no title, skipping", "comment_id", c.ID)
			continue
		}
		findings = append(findings, f)
	}
	return findings, nil
}

// qodoTitlePattern matches "N\. Title text <code>"
var qodoTitlePattern = regexp.MustCompile(`\d+\\?\.\s*(.+?)\s*<code>`)

// qodoFileRefPattern matches "path/to/file.ext[start-end]"
var qodoFileRefPattern = regexp.MustCompile(`([\w/.+-]+\.\w+)\[(\d+)-\d+\]`)

// qodoCategoryPriority maps Qodo quality dimension tags to our categories.
// Ordered by priority: first match wins when a comment contains multiple tags.
var qodoCategoryPriority = []struct {
	keyword  string
	category Category
}{
	{"Correctness", CategoryCorrectness},
	{"Security", CategorySecurity},
	{"Reliability", CategoryErrorHandling},
	{"Performance", CategoryPerformance},
	{"Observability", CategoryStyle},
	{"Architecture", CategoryStyle},
	{"Testability", CategoryStyle},
	{"Maintainability", CategoryStyle},
	{"Quality", CategoryStyle},
	{"Accessibility", CategoryStyle},
}

func parseQodoComment(body string) ReviewFinding {
	f := ReviewFinding{
		Source:   "qodo",
		Severity: SeverityLow,
		Category: CategoryCorrectness,
	}

	// Severity from alt text
	if strings.Contains(body, `alt="Action required"`) {
		f.Severity = SeverityCritical
	} else if strings.Contains(body, `alt="Suggestion"`) {
		f.Severity = SeverityMedium
	}

	// Title: strip HTML bold/italic tags from the match
	if m := qodoTitlePattern.FindStringSubmatch(body); m != nil {
		f.Title = stripHTMLTags(m[1])
	}

	// Category from quality dimension tags
	f.Category = extractQodoCategory(body)

	// Detail from <pre> block
	if start := strings.Index(body, "<pre>"); start >= 0 {
		if end := strings.Index(body[start:], "</pre>"); end >= 0 {
			detail := body[start+5 : start+end]
			f.Detail = strings.TrimSpace(stripHTMLTags(detail))
		}
	}

	// File/Line from Fix Focus Areas
	if m := qodoFileRefPattern.FindStringSubmatch(body); m != nil {
		f.File = m[1]
		if line, err := strconv.Atoi(m[2]); err == nil {
			f.Line = line
		}
	}

	return f
}

func extractQodoCategory(body string) Category {
	for _, entry := range qodoCategoryPriority {
		if strings.Contains(body, entry.keyword) {
			return entry.category
		}
	}
	return CategoryCorrectness
}

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

func stripHTMLTags(s string) string {
	return strings.TrimSpace(htmlTagPattern.ReplaceAllString(s, ""))
}
