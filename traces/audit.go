package traces

import (
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
