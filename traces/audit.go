package traces

import (
	"encoding/json"
	"fmt"

	"github.com/ruromero/la-fabriquilla/sandbox"
)

func AuditTrace(entry []byte) error {
	_, events := sandbox.RedactSecrets(string(entry))
	if len(events) > 0 {
		return fmt.Errorf("credential leak in trace: %v", events)
	}
	return nil
}

// AuditLogLine audits a slog JSON log line by extracting and unescaping
// the "trace" string field before scanning for credentials. This handles
// the double-encoding from traces.Log (json.Marshal → slog string attr).
func AuditLogLine(line []byte) error {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(line, &record); err != nil {
		return fmt.Errorf("invalid log line JSON: %w", err)
	}
	raw, ok := record["trace"]
	if !ok {
		return nil
	}
	var traceStr string
	if err := json.Unmarshal(raw, &traceStr); err != nil {
		return AuditTrace(raw)
	}
	return AuditTrace([]byte(traceStr))
}
