package traces

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	f, err := os.Open("testdata/golden_traces.jsonl")
	if err != nil {
		t.Fatalf("open golden file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := AuditTrace(line); err != nil {
			t.Errorf("golden entry %d flagged: %v", i, err)
		}
		i++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	if i == 0 {
		t.Fatal("golden file was empty")
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

func TestAuditLogLine_ExtractsAndScans(t *testing.T) {
	traceJSON := `{"issue_number":10,"phase":"coder","model":"qwen3:30b-a3b","prompt_tokens":1500,"completion_tokens":800,"tool_calls":3,"duration":"12.5s","started_at":"2025-06-20T10:00:00Z"}`
	slogLine := fmt.Sprintf(`{"time":"2025-06-20T10:00:12Z","level":"INFO","msg":"agent_trace","trace":%s}`, mustQuoteJSON(traceJSON))

	if err := AuditLogLine([]byte(slogLine)); err != nil {
		t.Errorf("clean slog line flagged: %v", err)
	}
}

func TestAuditLogLine_CatchesEscapedSecrets(t *testing.T) {
	tests := []struct {
		name  string
		trace string
	}{
		{"password in trace", `{"output":"password='hunter2!'"}`},
		{"GitHub token in trace", `{"output":"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"}`},
		{"bearer token in trace", `{"output":"Bearer eyJhbGciOiJIUzI1NiJ9.xxxxx.zzzzz"}`},
		{"connection string in trace", `{"output":"postgresql://admin:s3cret@db.internal:5432/prod"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slogLine := fmt.Sprintf(`{"time":"2025-06-20T10:00:12Z","level":"INFO","msg":"agent_trace","trace":%s}`, mustQuoteJSON(tt.trace))
			if err := AuditLogLine([]byte(slogLine)); err == nil {
				t.Errorf("AuditLogLine did not detect %s in slog line", tt.name)
			}
		})
	}
}

func TestAuditLogLine_NoTraceField(t *testing.T) {
	line := `{"time":"2025-06-20T10:00:12Z","level":"INFO","msg":"other_event","key":"value"}`
	if err := AuditLogLine([]byte(line)); err != nil {
		t.Errorf("line without trace field flagged: %v", err)
	}
}

func TestAuditLogLine_InvalidJSON(t *testing.T) {
	if err := AuditLogLine([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAuditLogLine_RealSlogOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)
	defer slog.SetDefault(slog.Default())

	Log(Trace{
		IssueNumber:  42,
		Phase:        "coder",
		Model:        "qwen3:30b-a3b",
		PromptTokens: 1500,
		CompTokens:   800,
		ToolCalls:    3,
		Duration:     "12.5s",
		StartedAt:    time.Date(2025, 6, 20, 10, 0, 0, 0, time.UTC),
	})

	line := bytes.TrimSpace(buf.Bytes())
	if len(line) == 0 {
		t.Fatal("Log produced no output")
	}

	if err := AuditLogLine(line); err != nil {
		t.Errorf("real slog output flagged: %v", err)
	}

	var record map[string]json.RawMessage
	if err := json.Unmarshal(line, &record); err != nil {
		t.Fatalf("slog output is not valid JSON: %v", err)
	}
	raw := record["trace"]
	var nested string
	if json.Unmarshal(raw, &nested) == nil {
		t.Error("trace field is still double-encoded as a string; expected a JSON object")
	}
}

// mustQuoteJSON returns s as a JSON-encoded string value (with escaping),
// simulating what slog's JSON handler does to string attributes.
func mustQuoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
