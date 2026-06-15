package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type Option func(*Client)

func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Tools          []Tool          `json:"tools,omitempty"`
	Stream         bool            `json:"stream"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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

type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict,omitempty"`
}

func StructuredOutput(schema any) *ResponseFormat {
	return &ResponseFormat{
		Type: "json_schema",
		JSONSchema: &JSONSchema{
			Name:   "output",
			Schema: schema,
		},
	}
}

// JSONOutput requests JSON output without a schema — compatible with all
// OpenAI-compatible endpoints including DeepSeek (which does not support
// json_schema). The caller is responsible for parsing and validating the
// returned JSON.
func JSONOutput() *ResponseFormat {
	return &ResponseFormat{Type: "json_object"}
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	ToolCallCount    int `json:"-"`
}

// ToolHandler executes tool calls from the model against an external system (e.g., MCP server).
type ToolHandler interface {
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req.Stream = false
	data, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("inference api: %d: %s", resp.StatusCode, body)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return chatResp, fmt.Errorf("inference api: empty choices in response")
	}
	return chatResp, nil
}

// ChatWithTools runs the tool-calling loop: sends a chat request, if the
// model responds with tool calls, executes them via the provided handler,
// appends results, and calls again until the model produces final content.
func (c *Client) ChatWithTools(ctx context.Context, req ChatRequest, handler ToolHandler, maxCalls int) (ChatResponse, error) {
	var totalPrompt, totalComp, totalToolCalls int
	var lastResp ChatResponse
	for range maxCalls {
		resp, err := c.Chat(ctx, req)
		if err != nil {
			lastResp.Usage.PromptTokens = totalPrompt
			lastResp.Usage.CompletionTokens = totalComp
			lastResp.Usage.ToolCallCount = totalToolCalls
			return lastResp, err
		}

		totalPrompt += resp.Usage.PromptTokens
		totalComp += resp.Usage.CompletionTokens
		lastResp = resp

		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			resp.Usage.PromptTokens = totalPrompt
			resp.Usage.CompletionTokens = totalComp
			resp.Usage.ToolCallCount = totalToolCalls
			return resp, nil
		}

		totalToolCalls += len(msg.ToolCalls)
		req.Messages = append(req.Messages, msg)

		for _, tc := range msg.ToolCalls {
			args, parseErr := parseArguments(tc.Function.Arguments)
			if parseErr != nil {
				slog.Warn("tool call argument parse failed", "tool", tc.Function.Name, "error", parseErr)
				req.Messages = append(req.Messages, Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: %v", parseErr),
				})
				continue
			}

			slog.Info("tool call", "tool", tc.Function.Name, "arg_keys", argKeys(args))
			result, execErr := handler.Execute(ctx, tc.Function.Name, args)
			if execErr != nil {
				slog.Warn("tool call failed", "tool", tc.Function.Name, "error", execErr)
				result = fmt.Sprintf("error: %v", execErr)
			} else {
				slog.Info("tool call result", "tool", tc.Function.Name, "result_len", len(result))
			}
			req.Messages = append(req.Messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	lastResp.Usage.PromptTokens = totalPrompt
	lastResp.Usage.CompletionTokens = totalComp
	lastResp.Usage.ToolCallCount = totalToolCalls
	return lastResp, fmt.Errorf("max tool calls (%d) exceeded", maxCalls)
}

// SimpleChat is a convenience wrapper for simple system+user message calls.
func (c *Client) SimpleChat(ctx context.Context, model, system, user string) (string, Usage, error) {
	temp := float64(0)
	resp, err := c.Chat(ctx, ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: &temp,
	})
	if err != nil {
		return "", Usage{}, err
	}
	return resp.Choices[0].Message.Content, resp.Usage, nil
}

func argKeys(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func parseArguments(raw string) (map[string]any, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}
	return args, nil
}
