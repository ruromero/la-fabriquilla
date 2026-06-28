package agents

import (
	"context"
	"fmt"

	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/sandbox"
)

const plannerSystemPrompt = `You are a software project planner. You are given a GitHub issue along with relevant project context that was gathered specifically for this issue.

CRITICAL RULES:
1. The project context describes what ALREADY EXISTS. The issue description may reference existing features as context — do not re-implement them. Only plan the DELTA: what needs to change or be added.
2. Do NOT generate code, SQL, or implementation details. The coder agent will make those decisions using the actual codebase. Your job is strategy and scope, not implementation.
3. Keep it concise. A good plan is a short list of what to change and why, not a tutorial.

You must do ONE of the following:

1. If the issue has enough information and is small enough for a single PR:
   Produce a plan as a numbered list of changes. For each step state:
   - WHAT to change (which file/module/layer)
   - WHY (the functional requirement it satisfies)
   - Whether it modifies existing code or adds new code
   - For guard clauses or validation checks, state the action on failure (return error, skip, log+continue, etc.)
   Do NOT include code snippets, SQL, API signatures, or framework-specific details.
   Start your response with "PLAN:" on the first line.

2. If the issue lacks critical information that CANNOT be discovered from the codebase:
   Only ask about business decisions, external credentials, or domain-specific requirements
   that no amount of code reading would answer. Never ask about implementation details
   like which framework, library, or templating engine is used — those are in the code.
   Start your response with "NEEDS_INFO:" on the first line.

3. If the issue is too complex for a single PR:
   Decompose it into smaller sub-issues, each independently implementable.
   List each sub-issue with a title, description, and dependency order.
   Start your response with "DECOMPOSE:" on the first line.

Prefer producing a plan with reasonable assumptions over asking for information.
Only use NEEDS_INFO for truly missing business context.`

type PlanResult struct {
	Outcome      string
	Content      string
	PromptTokens int
	CompTokens   int
	Model        string
}

func buildPlannerPrompt(issueTitle, issueBody, commentHistory, gatheredContext, conventions, researchContext, replanFeedback string) string {
	userPrompt := fmt.Sprintf("## Issue: %s\n\n%s", issueTitle, issueBody)
	if commentHistory != "" {
		userPrompt += fmt.Sprintf("\n\n## Discussion\n\nPrevious comments on this issue (may contain answers to earlier questions):\n\n%s", commentHistory)
	}
	if gatheredContext != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Context\n\n%s", gatheredContext)
	}
	if conventions != "" {
		userPrompt += fmt.Sprintf("\n\n## Project Conventions\n\nFollow these conventions in your plan:\n\n%s", conventions)
	}
	if researchContext != "" {
		userPrompt += fmt.Sprintf("\n\n## Research Context\n\n%s", researchContext)
	}
	if replanFeedback != "" {
		userPrompt += fmt.Sprintf("\n\n## Re-plan Feedback (from coder)\n\nThe previous plan was found infeasible during implementation. Reason:\n\n%s\n\nProduce a revised plan that accounts for this feedback.", sandbox.SanitizeInput(replanFeedback))
	}
	return userPrompt
}

func Plan(ctx context.Context, client *inference.Client, model, issueTitle, issueBody, researchContext, gatheredContext, conventions, commentHistory, replanFeedback string) (PlanResult, error) {
	if client == nil {
		return PlanResult{}, fmt.Errorf("planner requires an API client (configure planner in config.json and set PLANNER_API_KEY)")
	}

	userPrompt := buildPlannerPrompt(issueTitle, issueBody, commentHistory, gatheredContext, conventions, researchContext, replanFeedback)

	content, usage, err := client.SimpleChat(ctx, model, plannerSystemPrompt, userPrompt)
	if err != nil {
		return PlanResult{}, fmt.Errorf("planner: %w", err)
	}
	outcome := "plan"
	if len(content) > 11 && content[:11] == "NEEDS_INFO:" {
		outcome = "needs_info"
	} else if len(content) > 11 && content[:11] == "DECOMPOSE:" {
		outcome = "decompose"
	}

	return PlanResult{
		Outcome:      outcome,
		Content:      content,
		PromptTokens: usage.PromptTokens,
		CompTokens:   usage.CompletionTokens,
		Model:        model,
	}, nil
}
