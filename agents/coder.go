package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/ollama"
	"github.com/ruromero/la-fabriquilla/pipeline"
)

const coderModel = "qwen3:14b"

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
- Do not add unnecessary dependencies`

// CodeResult holds the coder output and token usage.
type CodeResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	ToolCalls    int
	Model        string
}

func Code(ctx context.Context, ol *ollama.Client, design, researchContext, conventions string, tools []ollama.Tool, handler ollama.ToolHandler) (string, error) {
	r, err := CodeWithUsage(ctx, ol, design, researchContext, conventions, tools, handler)
	return r.Content, err
}

// CodeWithUsage works like Code but also returns token usage.
func CodeWithUsage(ctx context.Context, ol *ollama.Client, design, researchContext, conventions string, tools []ollama.Tool, handler ollama.ToolHandler) (CodeResult, error) {
	userPrompt := fmt.Sprintf("## Technical Design\n\n%s", design)
	if conventions != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Conventions\n\nFollow these conventions strictly:\n\n%s", conventions)
	}
	if researchContext != "" {
		userPrompt += fmt.Sprintf("\n\n## Research Context\n\n%s", researchContext)
	}

	req := ollama.ChatRequest{
		Model: coderModel,
		Messages: []ollama.Message{
			{Role: "system", Content: coderSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Tools:   tools,
		Options: &ollama.Options{Temperature: 0},
	}

	var resp ollama.ChatResponse
	var err error
	if len(tools) > 0 && handler != nil {
		resp, err = ol.ChatWithTools(ctx, req, handler, 20)
		if err != nil {
			return CodeResult{}, fmt.Errorf("coder chat with tools: %w", err)
		}
	} else {
		req.Format = pipeline.GetCoderOutputSchema()
		resp, err = ol.Chat(ctx, req)
		if err != nil {
			return CodeResult{}, fmt.Errorf("coder chat: %w", err)
		}
	}

	return CodeResult{
		Content:      resp.Message.Content,
		PromptTokens: resp.PromptEvalCount,
		CompTokens:   resp.EvalCount,
		ToolCalls:    resp.ToolCallCount,
		Model:        coderModel,
	}, nil
}
