package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruromero/la-fabriquilla/eval"
)

func TestFilterCases(t *testing.T) {
	cases := []eval.TestCase{
		{Name: "a-one", Phase: "planner"},
		{Name: "b-two", Phase: "coder"},
		{Name: "a-three", Phase: "coder"},
	}
	got := filterCases(cases, "coder", "a-")
	if len(got) != 1 || got[0].Name != "a-three" {
		t.Errorf("got %+v", got)
	}
	got = filterCases(cases, "planner,coder", "")
	if len(got) != 3 {
		t.Errorf("phase list filter: got %d", len(got))
	}
	got = filterCases(cases, "", "")
	if len(got) != 3 {
		t.Errorf("no filter: got %d", len(got))
	}
}

func TestPreflightOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"name": "qwen2.5-coder:14b"}},
		})
	}))
	defer srv.Close()

	if err := preflightOllama(srv.URL+"/v1", "qwen2.5-coder:14b"); err != nil {
		t.Errorf("model present: %v", err)
	}
	if err := preflightOllama(srv.URL+"/v1", "missing-model"); err == nil {
		t.Error("missing model should fail preflight")
	}
}

func TestPreflightSkipsNonOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // no /api/tags endpoint
	}))
	defer srv.Close()
	if err := preflightOllama(srv.URL+"/v1", "anything"); err != nil {
		t.Errorf("non-Ollama endpoint should be skipped, got: %v", err)
	}
}
