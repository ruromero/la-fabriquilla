package review

import "encoding/json"

// DismissKey returns a composite key for deduplicating findings across
// arbiter iterations. Line is excluded because it can shift between edits.
// Uses JSON array encoding to avoid delimiter collisions.
func DismissKey(f ReviewFinding) string {
	b, _ := json.Marshal([]string{f.Source, string(f.Category), f.Title, f.File})
	return string(b)
}

type Classification string

const (
	ClassFixHere   Classification = "fix_here"
	ClassSubtask   Classification = "subtask"
	ClassRootCause Classification = "root_cause"
	ClassDismissed Classification = "dismissed"
)

type ArbiterFinding struct {
	Finding        ReviewFinding  `json:"finding"`
	Classification Classification `json:"classification"`
	Reason         string         `json:"reason"`
	ProposedTitle  string         `json:"proposed_title,omitempty"`
}

type ArbiterResult struct {
	Findings []ArbiterFinding `json:"findings"`
}
