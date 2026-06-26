package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruromero/la-fabriquilla/inference"
)

func TestDesignWithUsageNoTools(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inference.ChatResponse{
			Choices: []inference.Choice{{Message: inference.Message{Content: "## Design\n\nMock design output"}}},
			Usage:   inference.Usage{PromptTokens: 100, CompletionTokens: 50},
		})
	}))
	defer srv.Close()

	cl := inference.NewClient(srv.URL)
	result, err := DesignWithUsage(context.Background(), cl, "test-model", "plan", "", "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content == "" {
		t.Error("expected non-empty design content")
	}
	if result.ToolCalls != 0 {
		t.Errorf("expected 0 tool calls without tools, got %d", result.ToolCalls)
	}
}
