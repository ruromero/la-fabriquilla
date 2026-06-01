package harness

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ruromero/la-fabriquilla/inference"
)

type mockToolHandler struct {
	result string
	err    error
}

func (m *mockToolHandler) Execute(_ context.Context, _ string, _ map[string]any) (string, error) {
	return m.result, m.err
}

func TestCompositeRedactsSecrets(t *testing.T) {
	secret := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	mock := &mockToolHandler{result: "token is " + secret}

	c := NewCompositeToolHandler()
	c.Register([]inference.Tool{
		{Type: "function", Function: inference.ToolDef{Name: "read_file"}},
	}, mock)

	got, err := c.Execute(context.Background(), "read_file", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, secret) {
		t.Error("secret was not redacted from tool response")
	}
	if !strings.Contains(got, "[REDACTED:GitHub token]") {
		t.Errorf("result = %q, want to contain [REDACTED:GitHub token]", got)
	}
}

func TestCompositeRedactsErrors(t *testing.T) {
	secret := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	mock := &mockToolHandler{err: fmt.Errorf("auth failed with token %s", secret)}

	c := NewCompositeToolHandler()
	c.Register([]inference.Tool{
		{Type: "function", Function: inference.ToolDef{Name: "read_file"}},
	}, mock)

	_, err := c.Execute(context.Background(), "read_file", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Error("secret was not redacted from error message")
	}
	if !strings.Contains(err.Error(), "[REDACTED:GitHub token]") {
		t.Errorf("error = %q, want to contain [REDACTED:GitHub token]", err.Error())
	}
}

func TestCompositeUnknownTool(t *testing.T) {
	c := NewCompositeToolHandler()
	_, err := c.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("error = %q, want mention of unknown tool", err.Error())
	}
}

func TestCompositeCleanResult(t *testing.T) {
	clean := "package main\n\nfunc main() {}\n"
	mock := &mockToolHandler{result: clean}

	c := NewCompositeToolHandler()
	c.Register([]inference.Tool{
		{Type: "function", Function: inference.ToolDef{Name: "read_file"}},
	}, mock)

	got, err := c.Execute(context.Background(), "read_file", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != clean {
		t.Errorf("clean text was modified: %q", got)
	}
}
