package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ruromero/la-fabriquilla/eval"
	"github.com/ruromero/la-fabriquilla/inference"
)

// fakeOpenAI returns a server that always answers with content.
func fakeOpenAI(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func newTestAdapters(t *testing.T, srv *httptest.Server) *adapters {
	t.Helper()
	cl := inference.NewClient(srv.URL)
	return &adapters{
		agentClient: cl, agentModel: "test-model",
		arbClient: cl, arbModel: "test-arb",
		timeout: 30 * time.Second,
	}
}

func TestPlannerAdapterMapsOutcome(t *testing.T) {
	srv := fakeOpenAI(t, "<think>reason</think>PLAN:\n1. fix the nil check in config")
	defer srv.Close()
	a := newTestAdapters(t, srv)
	fn, ok := a.outputFunc("planner")
	if !ok {
		t.Fatal("planner adapter missing")
	}
	out, files, err := fn(eval.TestCase{Phase: "planner", Inputs: map[string]string{
		"issue_title": "t", "issue_body": "b", "conventions": "c",
	}}, 1)
	if err != nil {
		t.Fatalf("planner run: %v", err)
	}
	if files != nil {
		t.Errorf("planner should return no files")
	}
	if strings.Contains(out, "<think>") {
		t.Errorf("reasoning not stripped: %q", out)
	}
}

func TestCoderAdapterParsesFiles(t *testing.T) {
	srv := fakeOpenAI(t, `{"files":[{"path":"config/helpers.go","language":"go","content":"package config\nfunc Retry() {}"}]}`)
	defer srv.Close()
	a := newTestAdapters(t, srv)
	fn, _ := a.outputFunc("coder")
	out, files, err := fn(eval.TestCase{Phase: "coder", Inputs: map[string]string{
		"plan": "p", "gathered_context": "g", "conventions": "c",
	}}, 1)
	if err != nil {
		t.Fatalf("coder run: %v", err)
	}
	if len(files) != 1 || files[0].Path != "config/helpers.go" {
		t.Errorf("files = %+v", files)
	}
	if !strings.Contains(out, "Retry") {
		t.Errorf("output missing content: %q", out)
	}
}

func TestArbiterAdapterRendersClassifications(t *testing.T) {
	srv := fakeOpenAI(t, `{"findings":[{"finding":{"source":"qodo","severity":"low","category":"style","title":"Function too complex"},"classification":"dismissed","reason":"matches conventions"}]}`)
	defer srv.Close()
	a := newTestAdapters(t, srv)
	fn, ok := a.outputFunc("arbiter")
	if !ok {
		t.Fatal("arbiter adapter missing")
	}
	out, _, err := fn(eval.TestCase{Phase: "arbiter", Inputs: map[string]string{
		"qodo_findings":    `[{"category":"style","description":"Function too complex","file":"harness/composite.go"}]`,
		"factory_findings": `[]`,
		"conventions":      "complexity ok",
		"architecture":     "intentional",
	}}, 1)
	if err != nil {
		t.Fatalf("arbiter run: %v", err)
	}
	if !strings.Contains(out, "dismissed") {
		t.Errorf("classification missing from output: %q", out)
	}
}

func TestArbiterAdapterAbsentWithoutClient(t *testing.T) {
	a := &adapters{agentClient: inference.NewClient("http://x"), agentModel: "m"}
	if _, ok := a.outputFunc("arbiter"); ok {
		t.Error("arbiter adapter should be unavailable without arbiter client")
	}
}

func TestUnknownPhaseUnsupported(t *testing.T) {
	a := &adapters{}
	if _, ok := a.outputFunc("reviewer"); ok {
		t.Error("reviewer must be unsupported in v1")
	}
}
