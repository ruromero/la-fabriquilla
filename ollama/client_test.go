package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestChatPopulatesTokenCounts(t *testing.T) {
	resp := ChatResponse{
		Message:         Message{Role: "assistant", Content: "hello"},
		PromptEvalCount: 42,
		EvalCount:       18,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
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
	if got.Message.Content != "hello" {
		t.Errorf("content = %q, want %q", got.Message.Content, "hello")
	}
	if got.PromptEvalCount != 42 {
		t.Errorf("PromptEvalCount = %d, want 42", got.PromptEvalCount)
	}
	if got.EvalCount != 18 {
		t.Errorf("EvalCount = %d, want 18", got.EvalCount)
	}
}

func TestChatWithToolsAccumulatesTokens(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		switch n {
		case 1:
			// First call: model requests a tool call
			json.NewEncoder(w).Encode(ChatResponse{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{Function: ToolFunction{Name: "test_tool", Arguments: map[string]any{"key": "val"}}},
					},
				},
				PromptEvalCount: 100,
				EvalCount:       30,
			})
		case 2:
			// Second call: model requests another tool call
			json.NewEncoder(w).Encode(ChatResponse{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{Function: ToolFunction{Name: "test_tool", Arguments: map[string]any{"key": "val2"}}},
					},
				},
				PromptEvalCount: 150,
				EvalCount:       40,
			})
		default:
			// Final call: model returns content
			json.NewEncoder(w).Encode(ChatResponse{
				Message:         Message{Role: "assistant", Content: "final answer"},
				PromptEvalCount: 200,
				EvalCount:       50,
			})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	handler := &mockToolHandler{result: "tool output"}

	got, err := c.ChatWithTools(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []Tool{
			{Type: "function", Function: ToolDef{Name: "test_tool", Description: "a test tool"}},
		},
	}, handler, 10)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if got.Message.Content != "final answer" {
		t.Errorf("content = %q, want %q", got.Message.Content, "final answer")
	}

	// Token counts should be accumulated: 100+150+200 = 450, 30+40+50 = 120
	if got.PromptEvalCount != 450 {
		t.Errorf("accumulated PromptEvalCount = %d, want 450", got.PromptEvalCount)
	}
	if got.EvalCount != 120 {
		t.Errorf("accumulated EvalCount = %d, want 120", got.EvalCount)
	}
	// Tool call count should be accumulated: 1 + 1 = 2
	if got.ToolCallCount != 2 {
		t.Errorf("ToolCallCount = %d, want 2", got.ToolCallCount)
	}
}

func TestChatWithToolsNoToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Message:         Message{Role: "assistant", Content: "direct answer"},
			PromptEvalCount: 50,
			EvalCount:       25,
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
	if got.PromptEvalCount != 50 {
		t.Errorf("PromptEvalCount = %d, want 50", got.PromptEvalCount)
	}
	if got.EvalCount != 25 {
		t.Errorf("EvalCount = %d, want 25", got.EvalCount)
	}
}

func TestChatWithToolsMaxCallsReturnsAccumulatedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return a tool call, never final content
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Message: Message{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{Function: ToolFunction{Name: "test_tool", Arguments: map[string]any{"k": "v"}}},
				},
			},
			PromptEvalCount: 100,
			EvalCount:       50,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	got, err := c.ChatWithTools(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []Tool{
			{Type: "function", Function: ToolDef{Name: "test_tool", Description: "a test tool"}},
		},
	}, &mockToolHandler{result: "ok"}, 3)
	if err == nil {
		t.Fatal("expected max tool calls error")
	}

	// Should accumulate tokens from all 3 calls: 100*3 = 300, 50*3 = 150
	if got.PromptEvalCount != 300 {
		t.Errorf("PromptEvalCount = %d, want 300", got.PromptEvalCount)
	}
	if got.EvalCount != 150 {
		t.Errorf("EvalCount = %d, want 150", got.EvalCount)
	}
	// Tool calls: 1 per iteration * 3 iterations = 3
	if got.ToolCallCount != 3 {
		t.Errorf("ToolCallCount = %d, want 3", got.ToolCallCount)
	}
}

func TestChatRequestFormatMarshal(t *testing.T) {
	t.Run("with format field", func(t *testing.T) {
		req := ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hi"}},
			Format: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"files": map[string]any{"type": "array"},
				},
				"required": []string{"files"},
			},
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal raw: %v", err)
		}
		if _, ok := raw["format"]; !ok {
			t.Error("expected 'format' field in marshaled JSON")
		}
	})

	t.Run("without format field omits it", func(t *testing.T) {
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
		if _, ok := raw["format"]; ok {
			t.Error("expected 'format' field to be omitted when nil")
		}
	})
}

func TestChatRequestFormatSentToServer(t *testing.T) {
	var receivedFormat json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		receivedFormat = raw["format"]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Message:         Message{Role: "assistant", Content: `{"files":[]}`},
			PromptEvalCount: 10,
			EvalCount:       5,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"files": map[string]any{"type": "array"}},
		"required":   []string{"files"},
	}

	_, err := c.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Format:   schema,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if receivedFormat == nil {
		t.Fatal("server did not receive format field")
	}

	var parsed map[string]any
	if err := json.Unmarshal(receivedFormat, &parsed); err != nil {
		t.Fatalf("unmarshal format: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("format type = %v, want 'object'", parsed["type"])
	}
}

type mockToolHandler struct {
	result string
}

func (m *mockToolHandler) Execute(_ context.Context, name string, args map[string]any) (string, error) {
	return m.result, nil
}
