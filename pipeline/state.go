package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ruromero/la-fabriquilla/review"
)

type TokenUsage struct {
	Phase            string  `json:"phase"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	WallTimeSeconds  float64 `json:"wall_time_seconds"`
	ToolCalls        int     `json:"tool_calls,omitempty"`
}

// ModelCosts maps model identifiers to per-token cost in USD.
// Ollama models are zero-cost (local inference).
var ModelCosts = map[string]struct {
	PromptPerToken     float64
	CompletionPerToken float64
}{
	"gemini-2.5-flash":  {0.00000015, 0.0000006},
	"gemini-2.0-flash":  {0.00000010, 0.0000004},
	"deepseek-chat":     {0.00000014, 0.00000028},
	"deepseek-reasoner": {0.00000055, 0.00000219},
}

// EstimateCost returns the estimated USD cost for the given model and token counts.
func EstimateCost(model string, promptTokens, completionTokens int) float64 {
	rates, ok := ModelCosts[model]
	if !ok {
		return 0
	}
	return float64(promptTokens)*rates.PromptPerToken + float64(completionTokens)*rates.CompletionPerToken
}

type State struct {
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	IssueNumber int    `json:"issue_number"`

	Phase     string `json:"phase"`
	Iteration int    `json:"iteration"`

	IssueTitle     string `json:"issue_title"`
	IssueBody      string `json:"issue_body"`
	CommentHistory string `json:"comment_history,omitempty"`
	Summaries      string `json:"summaries"`
	Conventions    string `json:"conventions"`

	GatheredContext   string        `json:"gathered_context,omitempty"`
	ResearchContext   string        `json:"research_context,omitempty"`
	PlanOutcome       string        `json:"plan_outcome,omitempty"`
	PlanContent       string        `json:"plan_content,omitempty"`
	Design            string        `json:"design,omitempty"`
	Code              string        `json:"code,omitempty"`
	Review            *ReviewState  `json:"review,omitempty"`
	ArbiterResult     *ArbiterState `json:"arbiter_result,omitempty"`
	Files             []FileState   `json:"files,omitempty"`
	PhaseTokens       []TokenUsage  `json:"phase_tokens,omitempty"`
	TotalPromptTokens int           `json:"total_prompt_tokens,omitempty"`
	TotalCompTokens   int           `json:"total_completion_tokens,omitempty"`
	TotalCostUSD      float64       `json:"total_cost_usd,omitempty"`

	PRNumber int    `json:"pr_number,omitempty"`
	PRBranch string `json:"pr_branch,omitempty"`

	CloneDir string `json:"clone_dir,omitempty"`

	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ReviewState struct {
	Findings []review.ReviewFinding `json:"findings"`
}

// UnmarshalJSON handles both the current format (findings array) and the legacy
// format (correctness/security/intent strings) from in-flight pipeline states.
func (rs *ReviewState) UnmarshalJSON(data []byte) error {
	type current struct {
		Findings []review.ReviewFinding `json:"findings"`
	}
	var c current
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	if len(c.Findings) > 0 {
		rs.Findings = c.Findings
		return nil
	}
	var legacy struct {
		Correctness string `json:"correctness"`
		Security    string `json:"security"`
		Intent      string `json:"intent"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if legacy.Correctness != "" || legacy.Security != "" || legacy.Intent != "" {
		rs.Findings = append(rs.Findings, review.ParseReviewFindings(legacy.Correctness, review.CategoryCorrectness)...)
		rs.Findings = append(rs.Findings, review.ParseReviewFindings(legacy.Security, review.CategorySecurity)...)
		rs.Findings = append(rs.Findings, review.ParseIntentFindings(legacy.Intent)...)
	}
	return nil
}

type ArbiterState struct {
	Findings      []review.ArbiterFinding `json:"findings"`
	DismissedKeys []string                `json:"dismissed_keys,omitempty"`
}

// UnmarshalJSON handles both the current format (dismissed_keys) and the legacy
// format (dismissed_titles) from in-flight pipeline states.
func (as *ArbiterState) UnmarshalJSON(data []byte) error {
	type plain ArbiterState
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*as = ArbiterState(p)
	if len(as.DismissedKeys) == 0 {
		var legacy struct {
			DismissedTitles []string `json:"dismissed_titles"`
		}
		json.Unmarshal(data, &legacy)
		as.DismissedKeys = legacy.DismissedTitles
	}
	return nil
}

type FileState struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// RecordTokenUsage appends a phase token record and updates cumulative totals.
func (s *State) RecordTokenUsage(phase, model string, promptTokens, completionTokens, toolCalls int, wallTime float64) {
	total := promptTokens + completionTokens
	cost := EstimateCost(model, promptTokens, completionTokens)
	s.PhaseTokens = append(s.PhaseTokens, TokenUsage{
		Phase:            phase,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      total,
		EstimatedCostUSD: cost,
		WallTimeSeconds:  wallTime,
		ToolCalls:        toolCalls,
	})
	s.TotalPromptTokens += promptTokens
	s.TotalCompTokens += completionTokens
	s.TotalCostUSD += cost
}

func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

func SaveState(path string, s *State) error {
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename state file: %w", err)
	}
	return nil
}
