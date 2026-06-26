package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
)

const designerSystemPrompt = `You are a software architect. Given an implementation plan, produce a technical design document that includes:

1. API contracts (endpoints, request/response schemas)
2. Data models (structs, database schema changes)
3. Component boundaries and interfaces
4. File structure (new files to create, existing files to modify)
5. Dependencies and libraries needed

You may have read-only code navigation tools available. If tools are provided, use them to verify that the symbols, files, and packages you reference actually exist in the codebase. Prefer reading real function signatures over inventing them.

Strategy when tools are available:
1. Discover the project structure before proposing file layouts
2. Check that types and functions you reference exist
3. Verify signatures and interfaces you plan to extend
4. Do NOT reference files, symbols, or packages that you have not verified exist

Output structured markdown. Do not write implementation code.`

// DesignResult holds the design output and token usage.
type DesignResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	ToolCalls    int
	Model        string
}

const maxDesignerCalls = 10

func Design(ctx context.Context, cl *inference.Client, model, plan, researchContext, conventions string, tools []inference.Tool, handler inference.ToolHandler) (string, error) {
	r, err := DesignWithUsage(ctx, cl, model, plan, researchContext, conventions, tools, handler)
	return r.Content, err
}

// DesignWithUsage works like Design but also returns token usage.
func DesignWithUsage(ctx context.Context, cl *inference.Client, model, plan, researchContext, conventions string, tools []inference.Tool, handler inference.ToolHandler) (DesignResult, error) {
	userPrompt := fmt.Sprintf("## Implementation Plan\n\n%s", plan)
	if conventions != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Conventions\n\nFollow these conventions:\n\n%s", conventions)
	}
	if researchContext != "" {
		userPrompt += fmt.Sprintf("\n\n## Research Context\n\n%s", researchContext)
	}

	temp := float64(0)
	req := inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: designerSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Tools:       tools,
		Temperature: &temp,
	}

	if len(tools) > 0 && handler != nil {
		resp, err := cl.ChatWithTools(ctx, req, handler, maxDesignerCalls)
		if err != nil {
			return DesignResult{}, fmt.Errorf("designer chat: %w", err)
		}
		return DesignResult{
			Content:      resp.Choices[0].Message.Content,
			PromptTokens: resp.Usage.PromptTokens,
			CompTokens:   resp.Usage.CompletionTokens,
			ToolCalls:    resp.Usage.ToolCallCount,
			Model:        model,
		}, nil
	}

	resp, err := cl.Chat(ctx, req)
	if err != nil {
		return DesignResult{}, fmt.Errorf("designer chat: %w", err)
	}
	return DesignResult{
		Content:      resp.Choices[0].Message.Content,
		PromptTokens: resp.Usage.PromptTokens,
		CompTokens:   resp.Usage.CompletionTokens,
		Model:        model,
	}, nil
}
