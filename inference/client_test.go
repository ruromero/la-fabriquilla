package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestChatPopulatesTokenCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "hello"}}},
			Usage:   Usage{PromptTokens: 42, CompletionTokens: 18},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	got, err := c.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got.Choices[0].Message.Content != "hello" {
		t.Errorf("content = %q, want %q", got.Choices[0].Message.Content, "hello")
	}
	if got.Usage.PromptTokens != 42 {
		t.Errorf("PromptTokens = %d, want 42", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 18 {
		t.Errorf("CompletionTokens = %d, want 18", got.Usage.CompletionTokens)
	}
}

func TestChatWithToolsAccumulatesTokens(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		switch n {
		case 1:
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolFunction{
							Name:      "test_tool",
							Arguments: `{"key":"val"}`,
						},
					}},
				}}},
				Usage: Usage{PromptTokens: 100, CompletionTokens: 30},
			})
		case 2:
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:   "call_2",
						Type: "function",
						Function: ToolFunction{
							Name:      "test_tool",
							Arguments: `{"key":"val2"}`,
						},
					}},
				}}},
				Usage: Usage{PromptTokens: 150, CompletionTokens: 40},
			})
		default:
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "final answer"}}},
				Usage:   Usage{PromptTokens: 200, CompletionTokens: 50},
			})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	handler := &mockToolHandler{result: "tool output"}

	got, err := c.ChatWithTools(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []Tool{{Type: "function", Function: ToolDef{Name: "test_tool", Description: "a test tool"}}},
	}, handler, 10)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if got.Choices[0].Message.Content != "final answer" {
		t.Errorf("content = %q, want %q", got.Choices[0].Message.Content, "final answer")
	}
	if got.Usage.PromptTokens != 450 {
		t.Errorf("accumulated PromptTokens = %d, want 450", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 120 {
		t.Errorf("accumulated CompletionTokens = %d, want 120", got.Usage.CompletionTokens)
	}
	if got.Usage.ToolCallCount != 2 {
		t.Errorf("ToolCallCount = %d, want 2", got.Usage.ToolCallCount)
	}
}

func TestChatWithToolsEchoesToolCallID(t *testing.T) {
	var receivedMessages []Message

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)

		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if n == 1 {
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:       "call_abc123",
						Type:     "function",
						Function: ToolFunction{Name: "my_tool", Arguments: `{}`},
					}},
				}}},
			})
		} else {
			receivedMessages = req.Messages
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "done"}}},
			})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.ChatWithTools(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []Tool{{Type: "function", Function: ToolDef{Name: "my_tool"}}},
	}, &mockToolHandler{result: "ok"}, 5)

	found := false
	for _, m := range receivedMessages {
		if m.Role == "tool" && m.ToolCallID == "call_abc123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool result message with tool_call_id = call_abc123")
	}
}

func TestChatWithToolsNoToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "direct answer"}}},
			Usage:   Usage{PromptTokens: 50, CompletionTokens: 25},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	got, err := c.ChatWithTools(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, &mockToolHandler{}, 10)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if got.Usage.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d, want 50", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 25 {
		t.Errorf("CompletionTokens = %d, want 25", got.Usage.CompletionTokens)
	}
}

func TestChatWithToolsMaxCallsReturnsAccumulatedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: ToolFunction{Name: "test_tool", Arguments: `{"k":"v"}`},
				}},
			}}},
			Usage: Usage{PromptTokens: 100, CompletionTokens: 50},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	got, err := c.ChatWithTools(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []Tool{{Type: "function", Function: ToolDef{Name: "test_tool", Description: "a test tool"}}},
	}, &mockToolHandler{result: "ok"}, 3)
	if err == nil {
		t.Fatal("expected max tool calls error")
	}
	if got.Usage.PromptTokens != 300 {
		t.Errorf("PromptTokens = %d, want 300", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 150 {
		t.Errorf("CompletionTokens = %d, want 150", got.Usage.CompletionTokens)
	}
	if got.Usage.ToolCallCount != 3 {
		t.Errorf("ToolCallCount = %d, want 3", got.Usage.ToolCallCount)
	}
}

func TestChatAuthHeader(t *testing.T) {
	t.Run("with api key", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("auth header = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL, WithAPIKey("test-key"))
		c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	})

	t.Run("without api key", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("expected no auth header, got %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
			})
		}))
		defer srv.Close()

		c := NewClient(srv.URL)
		c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	})
}

func TestSimpleChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "test response"}}},
			Usage:   Usage{PromptTokens: 100, CompletionTokens: 50},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey("test-key"))

	text, usage, err := c.SimpleChat(context.Background(), "test-model", "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("SimpleChat: %v", err)
	}
	if text != "test response" {
		t.Errorf("text = %q, want %q", text, "test response")
	}
	if usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", usage.PromptTokens)
	}
	if usage.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", usage.CompletionTokens)
	}
}

func TestSimpleChatEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey("k"))
	_, _, err := c.SimpleChat(context.Background(), "m", "sys", "usr")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestSimpleChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey("k"))
	_, _, err := c.SimpleChat(context.Background(), "m", "sys", "usr")
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestResponseFormatMarshal(t *testing.T) {
	t.Run("with response_format", func(t *testing.T) {
		req := ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hi"}},
			ResponseFormat: StructuredOutput(map[string]any{
				"type":       "object",
				"properties": map[string]any{"files": map[string]any{"type": "array"}},
				"required":   []string{"files"},
			}),
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if _, ok := raw["response_format"]; !ok {
			t.Error("expected 'response_format' field in marshaled JSON")
		}
	})

	t.Run("without response_format omits it", func(t *testing.T) {
		req := ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hi"}},
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if _, ok := raw["response_format"]; ok {
			t.Error("expected 'response_format' field to be omitted when nil")
		}
	})
}

func TestChatEmptyChoicesReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/", WithAPIKey("k"))
	_, err := c.Chat(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat with trailing slash: %v", err)
	}
}

func TestArgKeysOmitsValues(t *testing.T) {
	got := argKeys(map[string]any{
		"path":    "/etc/passwd",
		"content": "secret-value",
	})
	if got != "content,path" {
		t.Errorf("argKeys = %q, want %q", got, "content,path")
	}
}

func TestParseArguments(t *testing.T) {
	args, err := parseArguments(`{"key":"value","num":42}`)
	if err != nil {
		t.Fatalf("parseArguments: %v", err)
	}
	if args["key"] != "value" {
		t.Errorf("key = %v, want %q", args["key"], "value")
	}
}

type mockToolHandler struct {
	result string
}

func (m *mockToolHandler) Execute(_ context.Context, name string, args map[string]any) (string, error) {
	return m.result, nil
}
