package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/pipeline"
)

const iteratorSystemPrompt = `You are a software developer applying review feedback to code.

Given the current code and a structured review with severity levels, apply fixes:
1. Address all [CRITICAL] issues first
2. Then address [MEDIUM] issues
3. [LOW] issues are optional

When you have finished all tool calls and are ready to output the final code,
respond with a JSON object in this exact format:
{
  "files": [
    {"path": "relative/path/to/file.go", "language": "go", "content": "full file content here"}
  ]
}

If JSON output is not possible, fall back to this format for each file:

FILE: path/to/file
` + "```" + `language
<complete file contents>
` + "```" + `

Do not introduce new features. Only fix the issues raised in the review.`

// IterateResult holds the iterate output and token usage.
type IterateResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	ToolCalls    int
	Model        string
}

func Iterate(ctx context.Context, cl *inference.Client, model, code, reviewFeedback string, tools []inference.Tool, handler inference.ToolHandler) (string, error) {
	r, err := IterateWithUsage(ctx, cl, model, code, reviewFeedback, tools, handler)
	return r.Content, err
}

// IterateWithUsage works like Iterate but also returns token usage.
func IterateWithUsage(ctx context.Context, cl *inference.Client, model, code, reviewFeedback string, tools []inference.Tool, handler inference.ToolHandler) (IterateResult, error) {
	userPrompt := fmt.Sprintf("## Current Code\n\n%s\n\n## Review Feedback\n\n%s", code, reviewFeedback)

	temp := float64(0)
	req := inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: iteratorSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Tools:       tools,
		Temperature: &temp,
	}

	var resp inference.ChatResponse
	var err error
	if len(tools) > 0 && handler != nil {
		resp, err = cl.ChatWithTools(ctx, req, handler, 20)
		if err != nil {
			return IterateResult{}, fmt.Errorf("iterate with tools: %w", err)
		}
	} else {
		req.ResponseFormat = inference.StructuredOutput(pipeline.GetCoderOutputSchema())
		resp, err = cl.Chat(ctx, req)
		if err != nil {
			return IterateResult{}, fmt.Errorf("iterate chat: %w", err)
		}
	}

	return IterateResult{
		Content:      resp.Choices[0].Message.Content,
		PromptTokens: resp.Usage.PromptTokens,
		CompTokens:   resp.Usage.CompletionTokens,
		ToolCalls:    resp.Usage.ToolCallCount,
		Model:        model,
	}, nil
}
