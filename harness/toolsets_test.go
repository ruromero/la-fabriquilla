package harness

import (
	"testing"

	"github.com/ruromero/la-fabriquilla/inference"
)

func TestBuildDesignerToolsNilSerena(t *testing.T) {
	tools, handler := BuildDesignerTools(nil, nil, nil)
	if handler == nil {
		t.Fatal("handler should not be nil even without Serena")
	}
	// Should return context tools only
	contextTools := ContextTools()
	if len(tools) != len(contextTools) {
		t.Errorf("expected %d context tools, got %d", len(contextTools), len(tools))
	}
}

func TestDesignerToolsExcludeWriteTools(t *testing.T) {
	allSerena := []inference.Tool{
		{Type: "function", Function: inference.ToolDef{Name: "find_symbol"}},
		{Type: "function", Function: inference.ToolDef{Name: "read_file"}},
		{Type: "function", Function: inference.ToolDef{Name: "list_dir"}},
		{Type: "function", Function: inference.ToolDef{Name: "replace_content"}},
		{Type: "function", Function: inference.ToolDef{Name: "replace_symbol_body"}},
	}

	filtered := FilterTools(allSerena, SerenaGatherAllowed)
	if len(filtered) != 3 {
		t.Errorf("expected 3 read-only tools, got %d", len(filtered))
	}
	for _, tool := range filtered {
		if tool.Function.Name == "replace_content" || tool.Function.Name == "replace_symbol_body" {
			t.Errorf("write tool %q should not be in designer toolset", tool.Function.Name)
		}
	}
}
