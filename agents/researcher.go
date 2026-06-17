package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
)

const researchPrompt = `You are a research assistant for a software development team. Given a GitHub issue and the project's tech stack, research and gather relevant context that will help a developer implement it.

IMPORTANT: Only provide examples and patterns that match the project's actual tech stack described below. Do NOT suggest libraries, frameworks, or patterns from other languages or ecosystems.

Focus on:
1. Relevant API documentation, library usage, and examples for the specific tech stack
2. Known patterns or solutions for this type of problem in this tech stack
3. Potential pitfalls or edge cases to watch out for
4. Any security considerations

Be concise and actionable. Structure your output as a reference document that a developer can use alongside the issue description.

%s

## Issue: %s

%s`

// ResearchResult holds the research output and token usage.
type ResearchResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	Model        string
}

func Research(ctx context.Context, cl *inference.Client, model, issueTitle, issueBody, techContext string) (string, error) {
	r, err := ResearchWithUsage(ctx, cl, model, issueTitle, issueBody, techContext)
	return r.Content, err
}

// ResearchWithUsage works like Research but also returns token usage metadata.
func ResearchWithUsage(ctx context.Context, cl *inference.Client, model, issueTitle, issueBody, techContext string) (ResearchResult, error) {
	if cl == nil {
		return ResearchResult{}, nil
	}

	stackSection := ""
	if techContext != "" {
		stackSection = fmt.Sprintf("## Project Tech Stack\n\n%s", techContext)
	}

	prompt := fmt.Sprintf(researchPrompt, stackSection, issueTitle, issueBody)
	content, usage, err := cl.SimpleChat(ctx, model, "", prompt)
	if err != nil {
		return ResearchResult{}, fmt.Errorf("research: %w", err)
	}
	return ResearchResult{
		Content:      content,
		PromptTokens: usage.PromptTokens,
		CompTokens:   usage.CompletionTokens,
		Model:        model,
	}, nil
}
