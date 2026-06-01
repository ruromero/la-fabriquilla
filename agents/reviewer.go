package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
)

const reviewerModel = "qwen3:14b"

const correctnessPrompt = `You are a senior engineer performing an adversarial code review. Your job is to find problems, not approve code.

Evaluate the code against the design and plan. For each issue found, output:

[CRITICAL] Title — description, affected files/lines
[MEDIUM] Title — description, affected files/lines
[LOW] Title — description, affected files/lines

Check for:
- Logic errors and edge cases
- Missing error handling
- Test adequacy (do tests verify behavior or just assert code runs?)
- Test integrity (were existing tests weakened?)
- API contract violations
- Missing documentation updates

If no issues found, output: [PASS] No issues found.`

const securityPrompt = `You are a security engineer reviewing code for vulnerabilities.

Check for:
- Injection vulnerabilities (SQL, command, XSS)
- Authentication/authorization bypasses
- Data exposure (PII, credentials, tokens in logs)
- Unsafe deserialization
- Dependency vulnerabilities
- Missing input validation at system boundaries

For each issue, output:
[CRITICAL] Title — description, CWE if applicable, affected files/lines
[MEDIUM] Title — description
[LOW] Title — description

If no security issues found, output: [PASS] No security issues found.`

const intentPrompt = `You are reviewing a PR for intent alignment and scope.

Given the original issue, the plan, and the code changes, check:
- Does the PR implement what the issue requested?
- Is there scope creep (changes beyond what was planned)?
- Are all planned items addressed?
- If documentation was affected by code changes, was it updated?

Output:
[ALIGNED] — if the PR matches the intent
[SCOPE_CREEP] Title — description of out-of-scope changes
[MISSING] Title — planned items not implemented
[DOCS_OUTDATED] Title — documentation not updated to match code changes`

type ReviewResult struct {
	Correctness  string
	Security     string
	Intent       string
	PromptTokens int
	CompTokens   int
	ToolCalls    int
	Model        string
}

func Review(ctx context.Context, cl *inference.Client, code, design, plan, conventions string, tools []inference.Tool, handler inference.ToolHandler) (ReviewResult, error) {
	codeContext := fmt.Sprintf("## Plan\n\n%s\n\n## Design\n\n%s\n\n## Code\n\n%s", plan, design, code)
	if conventions != "" {
		codeContext += fmt.Sprintf("\n\n## Project Conventions\n\nVerify code follows these conventions:\n\n%s", conventions)
	}

	var totalPrompt, totalComp, totalTools int
	var prompt, comp, tc int

	correctness, prompt, comp, tc, err := reviewWith(ctx, cl, correctnessPrompt, codeContext, tools, handler)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("correctness review: %w", err)
	}
	totalPrompt += prompt
	totalComp += comp
	totalTools += tc

	var security, intent string
	security, prompt, comp, tc, err = reviewWith(ctx, cl, securityPrompt, codeContext, tools, handler)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("security review: %w", err)
	}
	totalPrompt += prompt
	totalComp += comp
	totalTools += tc

	intent, prompt, comp, tc, err = reviewWith(ctx, cl, intentPrompt, codeContext, nil, nil)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("intent review: %w", err)
	}
	totalPrompt += prompt
	totalComp += comp
	totalTools += tc

	return ReviewResult{
		Correctness:  correctness,
		Security:     security,
		Intent:       intent,
		PromptTokens: totalPrompt,
		CompTokens:   totalComp,
		ToolCalls:    totalTools,
		Model:        reviewerModel,
	}, nil
}

func reviewWith(ctx context.Context, cl *inference.Client, systemPrompt, userContent string, tools []inference.Tool, handler inference.ToolHandler) (string, int, int, int, error) {
	temp := float64(0)
	req := inference.ChatRequest{
		Model: reviewerModel,
		Messages: []inference.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		Tools:       tools,
		Temperature: &temp,
	}

	if len(tools) > 0 && handler != nil {
		resp, err := cl.ChatWithTools(ctx, req, handler, 10)
		if err != nil {
			return "", 0, 0, 0, err
		}
		return resp.Choices[0].Message.Content, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.ToolCallCount, nil
	}

	resp, err := cl.Chat(ctx, req)
	if err != nil {
		return "", 0, 0, 0, err
	}
	return resp.Choices[0].Message.Content, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, 0, nil
}
