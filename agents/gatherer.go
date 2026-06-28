package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
)

const gathererSystemPrompt = `You are a context gathering agent for a software development planner.

Given a GitHub issue, you must gather enough project context to produce an accurate implementation plan. You have access to project documentation, source files, and code navigation tools.

IMPORTANT: You have these tools available:
- list_dir: List files and directories. Use this FIRST to discover the project structure.
- search_for_pattern: Search for text patterns (like grep). Use this to find keywords from the issue.
- read_file: Read a file's contents.
- find_symbol: Find symbol definitions by name.
- find_referencing_symbols: Find where a symbol is referenced.
- find_referencing_code_snippets: Find code snippets referencing a symbol.
- get_symbols_overview: Get an overview of symbols in a file.

Strategy — follow this order:
1. Start with list_dir (no arguments or with root path) to discover the project's directory structure
2. Extract key terms from the issue (e.g., "extend", "email", "borrowing") and use search_for_pattern to find where they appear in the codebase
3. Use read_file to read the files found by search_for_pattern
4. Use find_symbol and get_symbols_overview to understand the code structure of relevant files
5. Use find_referencing_symbols to trace how components connect to each other
6. Keep searching until you have a complete picture — use all 25 tool calls if needed

CRITICAL RULES:
- Do NOT guess file paths. Use list_dir and search_for_pattern to discover them.
- Do NOT stop after a few failed lookups. Try different search terms and approaches.
- If a tool returns no results, try a broader search (e.g., search for "email" instead of "email_template").
- Many issues describe changes to features that ALREADY EXIST. Search thoroughly before concluding something is missing.

The planner needs to know:
- What already exists: endpoints, handlers, database tables, UI components related to the issue
- What does NOT exist yet: the gap between current state and what the issue asks for
- Exact file paths, function signatures, data models, and API patterns
- How the existing system works (e.g., how emails are sent, how templates are stored)

When you have gathered enough context, produce a final response organized into:
1. EXISTING CODE: What already exists (with file paths, function names, schemas)
2. GAPS: What is missing or needs to change
3. PATTERNS: How similar features are currently implemented

Do NOT produce a plan — just gather and organize the context.`

const maxGatherCalls = 25

// GatherResult holds the gathered context and token usage.
type GatherResult struct {
	Content      string
	PromptTokens int
	CompTokens   int
	ToolCalls    int
	Model        string
}

func GatherContext(ctx context.Context, cl *inference.Client, model, issueTitle, issueBody, summaries string, tools []inference.Tool, handler inference.ToolHandler) (string, error) {
	r, err := GatherContextWithUsage(ctx, cl, model, issueTitle, issueBody, summaries, tools, handler)
	return r.Content, err
}

// GatherContextWithUsage works like GatherContext but also returns token usage.
func GatherContextWithUsage(ctx context.Context, cl *inference.Client, model, issueTitle, issueBody, summaries string, tools []inference.Tool, handler inference.ToolHandler) (GatherResult, error) {
	userPrompt := fmt.Sprintf("## Issue: %s\n\n%s\n\n## Project Summaries\n\n%s", issueTitle, issueBody, summaries)

	temp := float64(0)
	resp, err := cl.ChatWithTools(ctx, inference.ChatRequest{
		Model: model,
		Messages: []inference.Message{
			{Role: "system", Content: gathererSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Tools:       tools,
		Temperature: &temp,
	}, handler, maxGatherCalls)
	if err != nil {
		return GatherResult{}, fmt.Errorf("gatherer: %w", err)
	}

	return GatherResult{
		Content:      resp.Choices[0].Message.Content,
		PromptTokens: resp.Usage.PromptTokens,
		CompTokens:   resp.Usage.CompletionTokens,
		ToolCalls:    resp.Usage.ToolCallCount,
		Model:        model,
	}, nil
}
