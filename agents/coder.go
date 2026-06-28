package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/pipeline"
)

const coderSystemPrompt = `You are a software developer. Given a technical design, implement the code changes.

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
<file contents>
` + "```" + `

Rules:
- Write complete file contents, not patches
- Include tests
- Update documentation if behavior changes
- Follow existing code style and conventions
- Do not add unnecessary dependencies

Before writing any code, use your tools to verify the design's key assumptions:
- Do the files, functions, and types referenced in the design actually exist?
- Is the architecture consistent with what you observe in the codebase?

If the design cannot be implemented as specified — references nonexistent code,
conflicts with the actual architecture, or underestimates scope beyond a single PR —
respond with PLAN_INFEASIBLE: followed by a concrete explanation of what is wrong
and what the plan should account for instead. Do not attempt to write code.`

// CodeResult holds the coder output and token usage.
type CodeResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	ToolCalls    int
	Model        string
}

const planInfeasiblePrefix = "PLAN_INFEASIBLE:"

// ParseCoderOutput checks whether the coder response signals plan infeasibility.
func ParseCoderOutput(content string) (outcome, reason string) {
	if len(content) >= len(planInfeasiblePrefix) && content[:len(planInfeasiblePrefix)] == planInfeasiblePrefix {
		return "plan_infeasible", strings.TrimSpace(content[len(planInfeasiblePrefix):])
	}
	return "success", ""
}

func Code(ctx context.Context, cl *inference.Client, model, design, researchContext, conventions string, tools []inference.Tool, handler inference.ToolHandler) (string, error) {
	r, err := CodeWithUsage(ctx, cl, model, design, researchContext, conventions, tools, handler)
	return r.Content, err
}

// CodeWithUsage works like Code but also returns token usage.
func CodeWithUsage(ctx context.Context, cl *inference.Client, model, design, researchContext, conventions string, tools []inference.Tool, handler inference.ToolHandler) (CodeResult, error) {
	userPrompt := fmt.Sprintf("## Technical Design\n\n%s", design)
	if conventions != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Conventions\n\nFollow these conventions strictly:\n\n%s", conventions)
	}
	if researchContext != "" {
		userPrompt += fmt.Sprintf("\n\n## Research Context\n\n%s", researchContext)
	}

	temp := float64(0)
	req := inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: coderSystemPrompt},
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
			return CodeResult{}, fmt.Errorf("coder chat with tools: %w", err)
		}
	} else {
		req.ResponseFormat = inference.StructuredOutput(pipeline.GetCoderOutputSchema())
		resp, err = cl.Chat(ctx, req)
		if err != nil {
			return CodeResult{}, fmt.Errorf("coder chat: %w", err)
		}
	}

	return CodeResult{
		Content:      resp.Choices[0].Message.Content,
		PromptTokens: resp.Usage.PromptTokens,
		CompTokens:   resp.Usage.CompletionTokens,
		ToolCalls:    resp.Usage.ToolCallCount,
		Model:        model,
	}, nil
}
