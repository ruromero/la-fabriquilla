package traces

import (
	"log/slog"
	"time"
)

type Trace struct {
	IssueNumber     int       `json:"issue_number"`
	Phase           string    `json:"phase"`
	Model           string    `json:"model"`
	PromptTokens    int       `json:"prompt_tokens"`
	CompTokens      int       `json:"completion_tokens"`
	ToolCalls       int       `json:"tool_calls"`
	Duration        string    `json:"duration"`
	StartedAt       time.Time `json:"started_at"`
	CumPromptTokens int       `json:"cum_prompt_tokens,omitempty"`
	CumCompTokens   int       `json:"cum_completion_tokens,omitempty"`
	CumCostUSD      float64   `json:"cum_cost_usd,omitempty"`
}

func Log(t Trace) {
	slog.Info("agent_trace", "trace", slog.AnyValue(t))
}
