package github

import (
	"context"
	"fmt"
	"strings"
)

var requiredFiles = []string{
	"CODEOWNERS",
	"CONVENTIONS.md",
	"ARCHITECTURE.md",
	"README.md",
	".serena",
}

var agentInstructionFiles = []string{
	"CLAUDE.md",
	"AGENTS.md",
	"GEMINI.md",
	".github/copilot-instructions.md",
}

type ReadinessResult struct {
	Ready                 bool
	Missing               []string
	AgentInstructionsFile string
}

// CheckReadiness verifies the repo has the minimum required files
// before the factory will accept work from it.
func (c *Client) CheckReadiness(ctx context.Context) (ReadinessResult, error) {
	var missing []string

	// CODEOWNERS can be in root, .github/, or docs/
	codeownersFound := false
	for _, path := range []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"} {
		exists, err := c.FileExists(ctx, path)
		if err != nil {
			return ReadinessResult{}, fmt.Errorf("check %s: %w", path, err)
		}
		if exists {
			codeownersFound = true
			break
		}
	}
	if !codeownersFound {
		missing = append(missing, "CODEOWNERS")
	}

	for _, file := range requiredFiles {
		if strings.EqualFold(file, "CODEOWNERS") {
			continue
		}
		exists, err := c.FileExists(ctx, file)
		if err != nil {
			return ReadinessResult{}, fmt.Errorf("check %s: %w", file, err)
		}
		if !exists {
			missing = append(missing, file)
		}
	}

	// Any recognized agent instructions file satisfies the requirement
	agentFile := ""
	for _, file := range agentInstructionFiles {
		exists, err := c.FileExists(ctx, file)
		if err != nil {
			return ReadinessResult{}, fmt.Errorf("check %s: %w", file, err)
		}
		if exists {
			agentFile = file
			break
		}
	}
	if agentFile == "" {
		missing = append(missing, "agent instructions file (CLAUDE.md, AGENTS.md, GEMINI.md, or .github/copilot-instructions.md)")
	}

	return ReadinessResult{
		Ready:                 len(missing) == 0,
		Missing:               missing,
		AgentInstructionsFile: agentFile,
	}, nil
}
