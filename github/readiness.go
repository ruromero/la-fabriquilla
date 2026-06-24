package github

import (
	"context"
	"fmt"
)

type ReadinessResult struct {
	Ready   bool
	Missing []string
}

// CheckReadiness verifies the repo has the minimum required structural
// files before the factory will accept work from it. Context documents
// (README, ARCHITECTURE, etc.) are configured per repo via include_docs
// and loaded best-effort by the harness.
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

	exists, err := c.FileExists(ctx, ".serena")
	if err != nil {
		return ReadinessResult{}, fmt.Errorf("check .serena: %w", err)
	}
	if !exists {
		missing = append(missing, ".serena")
	}

	return ReadinessResult{
		Ready:   len(missing) == 0,
		Missing: missing,
	}, nil
}
