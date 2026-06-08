package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/review"
)

const arbiterSystemPrompt = `You are a senior engineering arbiter triaging code review findings.

You receive review findings from automated reviewers along with the project's conventions, architecture, and implementation plan. Your job is to classify each finding.

For EACH finding, assign exactly one classification:
- "fix_here": Simple fix that the iterator can apply in this PR. Use for clear bugs, missing error handling, or straightforward corrections.
- "subtask": Needs planning but belongs in this PR's scope. Use for findings that require multiple coordinated changes within the current work.
- "root_cause": Systemic issue that needs its own issue lifecycle. Use for architectural problems, repeated patterns, or cross-cutting concerns. Include a proposed_title for the new issue.
- "dismissed": Invalid given project context. Use when a finding conflicts with documented conventions, is a false positive, or is out of scope. Include the reason.

Guidelines:
- Low severity style findings should usually be dismissed unless they violate project conventions.
- A finding about a pattern that is explicitly documented in conventions should be dismissed.
- Scope creep findings should be classified as dismissed or root_cause, never fix_here.
- When in doubt between fix_here and subtask, prefer fix_here if the change is localized to one file.

Output a JSON object with a "findings" array. Each element must have: "finding" (the original finding object), "classification", "reason", and optionally "proposed_title" (required for root_cause).`

type ArbiterOutput struct {
	Result       review.ArbiterResult
	PromptTokens int
	CompTokens   int
	Model        string
}

func Arbitrate(ctx context.Context, cl *inference.Client, model string,
	findings []review.ReviewFinding, conventions, architecture, plan string,
	dismissedTitles []string) (ArbiterOutput, error) {

	remaining, autoDismissed := filterDismissedFindings(findings, dismissedTitles)

	if len(remaining) == 0 {
		return ArbiterOutput{
			Result: review.ArbiterResult{Findings: autoDismissed},
			Model:  model,
		}, nil
	}

	findingsJSON, err := json.Marshal(remaining)
	if err != nil {
		return ArbiterOutput{}, fmt.Errorf("marshal findings: %w", err)
	}

	userPrompt := fmt.Sprintf("## Findings\n\n%s", findingsJSON)
	if conventions != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Conventions\n\n%s", conventions)
	}
	if architecture != "" {
		userPrompt += fmt.Sprintf("\n\n## Architecture\n\n%s", architecture)
	}
	if plan != "" {
		userPrompt += fmt.Sprintf("\n\n## Implementation Plan\n\n%s", plan)
	}

	temp := float64(0)
	resp, err := cl.Chat(ctx, inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: arbiterSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature:    &temp,
		ResponseFormat: inference.StructuredOutput(arbiterOutputSchema),
	})
	if err != nil {
		return ArbiterOutput{}, fmt.Errorf("arbiter chat: %w", err)
	}

	result, err := parseArbiterResponse(resp.Choices[0].Message.Content)
	if err != nil {
		return ArbiterOutput{}, fmt.Errorf("parse arbiter response: %w", err)
	}

	result.Findings = append(result.Findings, autoDismissed...)

	return ArbiterOutput{
		Result:       result,
		PromptTokens: resp.Usage.PromptTokens,
		CompTokens:   resp.Usage.CompletionTokens,
		Model:        model,
	}, nil
}

var arbiterOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"findings": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"finding": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"source":   map[string]any{"type": "string"},
							"severity": map[string]any{"type": "string"},
							"category": map[string]any{"type": "string"},
							"title":    map[string]any{"type": "string"},
							"detail":   map[string]any{"type": "string"},
							"file":     map[string]any{"type": "string"},
							"line":     map[string]any{"type": "integer"},
						},
						"required": []string{"source", "severity", "category", "title"},
					},
					"classification": map[string]any{"type": "string", "enum": []string{"fix_here", "subtask", "root_cause", "dismissed"}},
					"reason":         map[string]any{"type": "string"},
					"proposed_title": map[string]any{"type": "string"},
				},
				"required": []string{"finding", "classification", "reason"},
			},
		},
	},
	"required": []string{"findings"},
}

func filterDismissedFindings(findings []review.ReviewFinding, dismissedKeys []string) (remaining []review.ReviewFinding, autoDismissed []review.ArbiterFinding) {
	if len(dismissedKeys) == 0 {
		return findings, nil
	}
	dismissed := make(map[string]bool, len(dismissedKeys))
	for _, k := range dismissedKeys {
		dismissed[k] = true
	}
	for _, f := range findings {
		if dismissed[review.DismissKey(f)] {
			autoDismissed = append(autoDismissed, review.ArbiterFinding{
				Finding:        f,
				Classification: review.ClassDismissed,
				Reason:         "auto-dismissed: reappeared after previous dismissal",
			})
		} else {
			remaining = append(remaining, f)
		}
	}
	return remaining, autoDismissed
}

func parseArbiterResponse(content string) (review.ArbiterResult, error) {
	var result review.ArbiterResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return review.ArbiterResult{}, fmt.Errorf("unmarshal arbiter result: %w", err)
	}
	for i, f := range result.Findings {
		switch f.Classification {
		case review.ClassFixHere, review.ClassSubtask, review.ClassRootCause, review.ClassDismissed:
		default:
			return review.ArbiterResult{}, fmt.Errorf("finding %d (%q): invalid classification %q", i, f.Finding.Title, f.Classification)
		}
		if f.Classification == review.ClassRootCause && f.ProposedTitle == "" {
			return review.ArbiterResult{}, fmt.Errorf("finding %d (%q): root_cause classification requires proposed_title", i, f.Finding.Title)
		}
	}
	return result, nil
}
