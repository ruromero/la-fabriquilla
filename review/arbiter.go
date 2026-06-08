package review

// DismissKey returns a composite key for deduplicating findings across
// arbiter iterations. Line is excluded because it can shift between edits.
func DismissKey(f ReviewFinding) string {
	return f.Source + "|" + string(f.Category) + "|" + f.Title + "|" + f.File
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
