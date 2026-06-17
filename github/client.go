package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticToken string

func (s staticToken) Token(_ context.Context) (string, error) {
	return string(s), nil
}

type Client struct {
	ts    TokenSource
	owner string
	repo  string
	http  *http.Client
}

type Issue struct {
	Number int     `json:"number"`
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	Labels []Label `json:"labels"`
}

type Label struct {
	Name string `json:"name"`
}

type Comment struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type FileChange struct {
	Path    string
	Content string
}

type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func NewClient(token, owner, repo string) *Client {
	return &Client{
		ts:    staticToken(token),
		owner: owner,
		repo:  repo,
		http:  &http.Client{},
	}
}

func NewClientWithAppAuth(auth *AppAuth, owner, repo string) *Client {
	return &Client{
		ts:    auth,
		owner: owner,
		repo:  repo,
		http:  &http.Client{},
	}
}

func (c *Client) Owner() string { return c.owner }
func (c *Client) Repo() string  { return c.repo }

func (c *Client) CloneShallow(ctx context.Context) (string, func(), error) {
	token, err := c.ts.Token(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("get token for clone: %w", err)
	}

	dir, err := os.MkdirTemp("", "factory-clone-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, c.owner, c.repo)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, dir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("git clone: %w", err)
	}

	return dir, cleanup, nil
}

func (c *Client) GetRaw(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if err := c.setAuth(ctx, req); err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github api GET %s: %d: %s", url, resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Client) ListIssuesByLabel(ctx context.Context, label string) ([]Issue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?labels=%s&state=open", c.owner, c.repo, label)
	var issues []Issue
	if err := c.get(ctx, url, &issues); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return issues, nil
}

func (c *Client) AddLabel(ctx context.Context, issueNumber int, label string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/labels", c.owner, c.repo, issueNumber)
	body := map[string][]string{"labels": {label}}
	return c.post(ctx, url, body, nil)
}

func (c *Client) RemoveLabel(ctx context.Context, issueNumber int, label string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/labels/%s", c.owner, c.repo, issueNumber, label)
	return c.delete(ctx, url)
}

func (c *Client) CreateComment(ctx context.Context, issueNumber int, body string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", c.owner, c.repo, issueNumber)
	payload := map[string]string{"body": body}
	return c.post(ctx, url, payload, nil)
}

func (c *Client) ListComments(ctx context.Context, issueNumber int) ([]Comment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", c.owner, c.repo, issueNumber)
	var comments []Comment
	if err := c.get(ctx, url, &comments); err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return comments, nil
}

func (c *Client) ListPRReviewComments(ctx context.Context, prNumber int) ([]Comment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/comments", c.owner, c.repo, prNumber)
	var comments []Comment
	if err := c.get(ctx, url, &comments); err != nil {
		return nil, fmt.Errorf("list pr review comments: %w", err)
	}
	return comments, nil
}

// PRReview represents a GitHub pull request review.
type PRReview struct {
	ID   int    `json:"id"`
	Body string `json:"body"`
	// State is one of APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, PENDING.
	State string `json:"state"`
	User  struct {
		Login string `json:"login"`
	} `json:"user"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (c *Client) ListPRReviews(ctx context.Context, prNumber int) ([]PRReview, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews", c.owner, c.repo, prNumber)
	var reviews []PRReview
	if err := c.get(ctx, url, &reviews); err != nil {
		return nil, fmt.Errorf("list pr reviews: %w", err)
	}
	return reviews, nil
}

func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (Issue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", c.owner, c.repo)
	payload := map[string]any{
		"title":  title,
		"body":   body,
		"labels": labels,
	}
	var issue Issue
	if err := c.post(ctx, url, payload, &issue); err != nil {
		return Issue{}, fmt.Errorf("create issue: %w", err)
	}
	return issue, nil
}

func (c *Client) GetFileContent(ctx context.Context, path string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", c.owner, c.repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")

	if err := c.setAuth(ctx, req); err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", fmt.Errorf("file not found: %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get file %s: %d: %s", path, resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Client) FileExists(ctx context.Context, path string) (bool, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", c.owner, c.repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, err
	}

	if err := c.setAuth(ctx, req); err != nil {
		return false, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		return true, nil
	}
	if resp.StatusCode == 404 {
		return false, nil
	}
	return false, fmt.Errorf("check file %s: status %d", path, resp.StatusCode)
}

func (c *Client) GetBranchSHA(ctx context.Context, branch string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/heads/%s", c.owner, c.repo, branch)
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.get(ctx, url, &ref); err != nil {
		return "", fmt.Errorf("get branch SHA: %w", err)
	}
	return ref.Object.SHA, nil
}

func (c *Client) CreateBranch(ctx context.Context, branchName, sha string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs", c.owner, c.repo)
	payload := map[string]string{
		"ref": "refs/heads/" + branchName,
		"sha": sha,
	}
	return c.post(ctx, url, payload, nil)
}

func (c *Client) CreateCommit(ctx context.Context, branch, message string, files []FileChange) (string, error) {
	refURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/heads/%s", c.owner, c.repo, branch)
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.get(ctx, refURL, &ref); err != nil {
		return "", fmt.Errorf("get branch ref: %w", err)
	}
	commitSHA := ref.Object.SHA

	commitURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits/%s", c.owner, c.repo, commitSHA)
	var commitObj struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.get(ctx, commitURL, &commitObj); err != nil {
		return "", fmt.Errorf("get commit tree: %w", err)
	}

	type treeEntry struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	entries := make([]treeEntry, len(files))
	for i, f := range files {
		entries[i] = treeEntry{Path: f.Path, Mode: "100644", Type: "blob", Content: f.Content}
	}

	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees", c.owner, c.repo)
	treePayload := map[string]any{
		"base_tree": commitObj.Tree.SHA,
		"tree":      entries,
	}
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := c.post(ctx, treeURL, treePayload, &newTree); err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	newCommitURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits", c.owner, c.repo)
	newCommitPayload := map[string]any{
		"message": message,
		"tree":    newTree.SHA,
		"parents": []string{commitSHA},
	}
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := c.post(ctx, newCommitURL, newCommitPayload, &newCommit); err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	if err := c.patch(ctx, refURL, map[string]string{"sha": newCommit.SHA}, nil); err != nil {
		return "", fmt.Errorf("update branch ref: %w", err)
	}

	return newCommit.SHA, nil
}

func (c *Client) CreatePullRequest(ctx context.Context, title, body, head, base string) (PullRequest, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", c.owner, c.repo)
	payload := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}
	var pr PullRequest
	if err := c.post(ctx, url, payload, &pr); err != nil {
		return PullRequest{}, fmt.Errorf("create PR: %w", err)
	}
	return pr, nil
}

func (c *Client) get(ctx context.Context, url string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return c.do(ctx, req, result)
}

func (c *Client) post(ctx context.Context, url string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	return c.do(ctx, req, result)
}

func (c *Client) delete(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	return c.do(ctx, req, nil)
}

func (c *Client) patch(ctx context.Context, url string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	return c.do(ctx, req, result)
}

func (c *Client) setAuth(ctx context.Context, req *http.Request) error {
	token, err := c.ts.Token(ctx)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return nil
}

func (c *Client) do(ctx context.Context, req *http.Request, result any) error {
	if err := c.setAuth(ctx, req); err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api %s %s: %d: %s", req.Method, req.URL.Path, resp.StatusCode, body)
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}
