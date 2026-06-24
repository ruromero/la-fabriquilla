package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/ruromero/la-fabriquilla/github"
)

// MemoryClient is an in-memory implementation of github.Service for testing.
type MemoryClient struct {
	mu     sync.Mutex
	owner  string
	repo   string
	issues map[int]*github.Issue
	// Track operations for assertions
	Comments   []RecordedComment
	CreatedPRs []RecordedPR
	nextPR     int
	files      map[string]string
}

type RecordedComment struct {
	IssueNumber int
	Body        string
}

type RecordedPR struct {
	github.PullRequest
	Title string
	Body  string
	Head  string
	Base  string
}

type MemoryOption func(*MemoryClient)

func WithIssue(issue github.Issue) MemoryOption {
	return func(mc *MemoryClient) {
		mc.issues[issue.Number] = &issue
	}
}

func WithFile(path, content string) MemoryOption {
	return func(mc *MemoryClient) {
		mc.files[path] = content
	}
}

func NewMemoryClient(owner, repo string, opts ...MemoryOption) *MemoryClient {
	mc := &MemoryClient{
		owner:  owner,
		repo:   repo,
		issues: make(map[int]*github.Issue),
		nextPR: 1,
		files:  make(map[string]string),
	}
	for _, opt := range opts {
		opt(mc)
	}
	return mc
}

func (mc *MemoryClient) Owner() string { return mc.owner }
func (mc *MemoryClient) Repo() string  { return mc.repo }

func (mc *MemoryClient) ListIssuesByLabel(_ context.Context, label string) ([]github.Issue, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	var result []github.Issue
	for _, issue := range mc.issues {
		for _, l := range issue.Labels {
			if l.Name == label {
				result = append(result, *issue)
				break
			}
		}
	}
	return result, nil
}

func (mc *MemoryClient) AddLabel(_ context.Context, issueNumber int, label string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	issue, ok := mc.issues[issueNumber]
	if !ok {
		return fmt.Errorf("issue %d not found", issueNumber)
	}
	for _, l := range issue.Labels {
		if l.Name == label {
			return nil
		}
	}
	issue.Labels = append(issue.Labels, github.Label{Name: label})
	return nil
}

func (mc *MemoryClient) RemoveLabel(_ context.Context, issueNumber int, label string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	issue, ok := mc.issues[issueNumber]
	if !ok {
		return fmt.Errorf("issue %d not found", issueNumber)
	}
	var labels []github.Label
	for _, l := range issue.Labels {
		if l.Name != label {
			labels = append(labels, l)
		}
	}
	issue.Labels = labels
	return nil
}

func (mc *MemoryClient) CreateComment(_ context.Context, issueNumber int, body string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Comments = append(mc.Comments, RecordedComment{IssueNumber: issueNumber, Body: body})
	return nil
}

func (mc *MemoryClient) ListComments(_ context.Context, _ int) ([]github.Comment, error) {
	return nil, nil
}

func (mc *MemoryClient) CreateIssue(_ context.Context, title, body string, labels []string) (github.Issue, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	num := len(mc.issues) + 100
	lbls := make([]github.Label, len(labels))
	for i, l := range labels {
		lbls[i] = github.Label{Name: l}
	}
	issue := github.Issue{Number: num, Title: title, Body: body, Labels: lbls}
	mc.issues[num] = &issue
	return issue, nil
}

func (mc *MemoryClient) FileExists(_ context.Context, path string) (bool, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	_, ok := mc.files[path]
	return ok, nil
}

func (mc *MemoryClient) GetFileContent(_ context.Context, path string) (string, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	content, ok := mc.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	return content, nil
}

func (mc *MemoryClient) CheckReadiness(_ context.Context) (github.ReadinessResult, error) {
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md", ".github/copilot-instructions.md"} {
		if _, ok := mc.files[name]; ok {
			return github.ReadinessResult{Ready: true, AgentInstructionsFile: name}, nil
		}
	}
	return github.ReadinessResult{Ready: true, AgentInstructionsFile: ""}, nil
}

func (mc *MemoryClient) CloneShallow(_ context.Context) (string, func(), error) {
	return "", func() {}, nil
}

func (mc *MemoryClient) GetBranchSHA(_ context.Context, _ string) (string, error) {
	return "abc123fake", nil
}

func (mc *MemoryClient) CreateBranch(_ context.Context, _, _ string) error {
	return nil
}

func (mc *MemoryClient) CreateCommit(_ context.Context, _, _ string, _ []github.FileChange) (string, error) {
	return "def456fake", nil
}

func (mc *MemoryClient) CreatePullRequest(_ context.Context, title, body, head, base string) (github.PullRequest, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	pr := github.PullRequest{
		Number:  mc.nextPR,
		HTMLURL: fmt.Sprintf("https://github.com/%s/%s/pull/%d", mc.owner, mc.repo, mc.nextPR),
	}
	mc.nextPR++
	mc.CreatedPRs = append(mc.CreatedPRs, RecordedPR{
		PullRequest: pr,
		Title:       title,
		Body:        body,
		Head:        head,
		Base:        base,
	})
	return pr, nil
}

func (mc *MemoryClient) ListPRReviewComments(_ context.Context, _ int) ([]github.Comment, error) {
	return nil, nil
}

func (mc *MemoryClient) ListPRReviews(_ context.Context, _ int) ([]github.PRReview, error) {
	return nil, nil
}

// Labels returns the current labels for an issue (for test assertions).
func (mc *MemoryClient) Labels(issueNumber int) []string {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	issue, ok := mc.issues[issueNumber]
	if !ok {
		return nil
	}
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.Name
	}
	return labels
}
