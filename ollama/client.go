package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	Options  *Options  `json:"options,omitempty"`
	Format   any       `json:"format,omitempty"` // JSON Schema for structured output
}

type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Tool struct {
	Type     string  `json:"type"`
	Function ToolDef `json:"function"`
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type Options struct {
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
}

type ChatResponse struct {
	Message         Message `json:"message"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	ToolCallCount   int     `json:"-"`
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req.Stream = false
	data, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("ollama chat: %d: %s", resp.StatusCode, body)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return chatResp, nil
}

// ChatWithTools runs the tool-calling loop: sends a chat request, if the
// model responds with tool calls, executes them via the provided handler,
// appends results, and calls again until the model produces final content.
func (c *Client) ChatWithTools(ctx context.Context, req ChatRequest, handler ToolHandler, maxCalls int) (ChatResponse, error) {
	var totalPromptEval, totalEval, totalToolCalls int
	var lastResp ChatResponse
	for range maxCalls {
		resp, err := c.Chat(ctx, req)
		if err != nil {
			lastResp.PromptEvalCount = totalPromptEval
			lastResp.EvalCount = totalEval
			lastResp.ToolCallCount = totalToolCalls
			return lastResp, err
		}

		totalPromptEval += resp.PromptEvalCount
		totalEval += resp.EvalCount
		lastResp = resp

		if len(resp.Message.ToolCalls) == 0 {
			resp.PromptEvalCount = totalPromptEval
			resp.EvalCount = totalEval
			resp.ToolCallCount = totalToolCalls
			return resp, nil
		}

		totalToolCalls += len(resp.Message.ToolCalls)
		req.Messages = append(req.Messages, resp.Message)

		for _, tc := range resp.Message.ToolCalls {
			slog.Info("tool call", "tool", tc.Function.Name, "args", tc.Function.Arguments)
			result, err := handler.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				slog.Warn("tool call failed", "tool", tc.Function.Name, "error", err)
				result = fmt.Sprintf("error: %v", err)
			} else {
				slog.Info("tool call result", "tool", tc.Function.Name, "result_len", len(result))
			}
			req.Messages = append(req.Messages, Message{
				Role:    "tool",
				Content: result,
			})
		}
	}

	lastResp.PromptEvalCount = totalPromptEval
	lastResp.EvalCount = totalEval
	lastResp.ToolCallCount = totalToolCalls
	return lastResp, fmt.Errorf("max tool calls (%d) exceeded", maxCalls)
}

// ToolHandler executes tool calls from the model against an external system (e.g., MCP server).
type ToolHandler interface {
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}
