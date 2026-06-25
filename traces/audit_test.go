package traces

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuditTrace_CatchesCredentials(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
	}{
		{"API key in output", "output", "config: api_key=sk-abc123def456ghi789jkl"},
		{"bearer token in output", "output", "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.xxxxx.zzzzz"},
		{"private key block", "output", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----"},
		{"connection string", "output", "postgresql://admin:s3cret@db.internal:5432/prod"},
		{"GitHub token", "output", "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"},
		{"AWS key", "output", "key: AKIAIOSFODNN7EXAMPLE"},
		{"password assignment", "output", "password='hunter2!'"},
		{"generic secret env", "output", "GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx"},
		{"IPv4 address", "output", "connecting to 192.168.1.100"},
		{"internal hostname", "output", "resolved db.internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := map[string]string{tt.field: tt.value}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := AuditTrace(data); err == nil {
				t.Errorf("AuditTrace did not detect %s", tt.name)
			}
		})
	}
}

func TestAuditTrace_CleanTrace(t *testing.T) {
	trace := Trace{
		IssueNumber:  42,
		Phase:        "coder",
		Model:        "qwen3:30b-a3b",
		PromptTokens: 1500,
		CompTokens:   800,
		ToolCalls:    3,
		Duration:     "12.5s",
		StartedAt:    time.Date(2025, 6, 20, 10, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := AuditTrace(data); err != nil {
		t.Errorf("clean trace flagged: %v", err)
	}
}

func TestAuditTrace_CleanGoldenFile(t *testing.T) {
	golden := []string{
		`{"issue_number":10,"phase":"planner","model":"qwen3:30b-a3b","prompt_tokens":2000,"completion_tokens":500,"tool_calls":0,"duration":"5.2s","started_at":"2025-06-20T10:00:00Z"}`,
		`{"issue_number":10,"phase":"designer","model":"qwen3:30b-a3b","prompt_tokens":3000,"completion_tokens":1200,"tool_calls":2,"duration":"18.1s","started_at":"2025-06-20T10:01:00Z"}`,
		`{"issue_number":10,"phase":"coder","model":"qwen3:30b-a3b","prompt_tokens":4500,"completion_tokens":2000,"tool_calls":5,"duration":"45.3s","started_at":"2025-06-20T10:02:00Z"}`,
		`{"issue_number":10,"phase":"reviewer","model":"gemma3:27b","prompt_tokens":3500,"completion_tokens":800,"tool_calls":1,"duration":"12.0s","started_at":"2025-06-20T10:03:00Z"}`,
	}
	for i, entry := range golden {
		if err := AuditTrace([]byte(entry)); err != nil {
			t.Errorf("golden entry %d flagged: %v", i, err)
		}
	}
}

func TestAuditTrace_MultipleLeaks(t *testing.T) {
	entry := map[string]string{
		"output": "password = \"s3cret!\" and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
	}
	data, _ := json.Marshal(entry)
	err := AuditTrace(data)
	if err == nil {
		t.Fatal("expected error for multiple leaks")
	}
	msg := err.Error()
	if !strings.Contains(msg, "credential leak") {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestAuditTrace_EmptyEntry(t *testing.T) {
	if err := AuditTrace([]byte("{}")); err != nil {
		t.Errorf("empty entry flagged: %v", err)
	}
}
