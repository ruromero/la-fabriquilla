package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
)

const designerModel = "qwen3:14b"

const designerSystemPrompt = `You are a software architect. Given an implementation plan, produce a technical design document that includes:

1. API contracts (endpoints, request/response schemas)
2. Data models (structs, database schema changes)
3. Component boundaries and interfaces
4. File structure (new files to create, existing files to modify)
5. Dependencies and libraries needed

Output structured markdown. Do not write implementation code.`

// DesignResult holds the design output and token usage.
type DesignResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	Model        string
}

func Design(ctx context.Context, cl *inference.Client, plan, researchContext, conventions string) (string, error) {
	r, err := DesignWithUsage(ctx, cl, plan, researchContext, conventions)
	return r.Content, err
}

// DesignWithUsage works like Design but also returns token usage.
func DesignWithUsage(ctx context.Context, cl *inference.Client, plan, researchContext, conventions string) (DesignResult, error) {
	userPrompt := fmt.Sprintf("## Implementation Plan\n\n%s", plan)
	if conventions != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Conventions\n\nFollow these conventions:\n\n%s", conventions)
	}
	if researchContext != "" {
		userPrompt += fmt.Sprintf("\n\n## Research Context\n\n%s", researchContext)
	}

	temp := float64(0)
	resp, err := cl.Chat(ctx, inference.ChatRequest{
		Model: designerModel,
		Messages: []inference.Message{
			{Role: "system", Content: designerSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: &temp,
	})
	if err != nil {
		return DesignResult{}, fmt.Errorf("designer chat: %w", err)
	}

	return DesignResult{
		Content:      resp.Choices[0].Message.Content,
		PromptTokens: resp.Usage.PromptTokens,
		CompTokens:   resp.Usage.CompletionTokens,
		Model:        designerModel,
	}, nil
}
