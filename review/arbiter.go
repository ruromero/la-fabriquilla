package review

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
