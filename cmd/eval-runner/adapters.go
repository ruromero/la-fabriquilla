package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	"github.com/ruromero/la-fabriquilla/eval"
	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/review"
)

// adapters builds per-phase OutputFuncE values that call real agents.
// Token counters accumulate across the runs of one case; the runner
// resets them per case via takeUsage.
type adapters struct {
	agentClient *inference.Client
	agentModel  string
	arbClient   *inference.Client
	arbModel    string
	timeout     time.Duration

	promptTokens int
	compTokens   int
}

func (a *adapters) addUsage(prompt, comp int) {
	a.promptTokens += prompt
	a.compTokens += comp
}

// takeUsage returns accumulated token counts and resets them.
func (a *adapters) takeUsage() (prompt, comp int) {
	prompt, comp = a.promptTokens, a.compTokens
	a.promptTokens, a.compTokens = 0, 0
	return prompt, comp
}

// outputFunc returns the adapter for a phase, or ok=false when the phase
// is not supported in v1 (reviewer, gatherer) or not configured (arbiter
// without endpoint).
func (a *adapters) outputFunc(phase string) (eval.OutputFuncE, bool) {
	switch phase {
	case "planner":
		return a.runPlanner, true
	case "designer":
		return a.runDesigner, true
	case "coder":
		return a.runCoder, true
	case "arbiter":
		if a.arbClient == nil || a.arbModel == "" {
			return nil, false
		}
		return a.runArbiter, true
	default:
		return nil, false
	}
}

func (a *adapters) runCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), a.timeout)
}

func (a *adapters) runPlanner(tc eval.TestCase, _ int) (string, []eval.FileState, error) {
	ctx, cancel := a.runCtx()
	defer cancel()
	in := tc.Inputs
	res, err := agents.Plan(ctx, a.agentClient, a.agentModel,
		in["issue_title"], in["issue_body"], in["research_context"],
		in["gathered_context"], in["conventions"], in["comment_history"])
	if err != nil {
		return "", nil, err
	}
	a.addUsage(res.PromptTokens, res.CompTokens)
	return eval.StripReasoning(res.Outcome + "\n" + res.Content), nil, nil
}

func (a *adapters) runDesigner(tc eval.TestCase, _ int) (string, []eval.FileState, error) {
	ctx, cancel := a.runCtx()
	defer cancel()
	in := tc.Inputs
	res, err := agents.DesignWithUsage(ctx, a.agentClient, a.agentModel,
		in["plan"], in["research_context"], in["conventions"])
	if err != nil {
		return "", nil, err
	}
	a.addUsage(res.PromptTokens, res.CompTokens)
	return eval.StripReasoning(res.Content), nil, nil
}

// runCoder exercises the no-tools structured-JSON mode. Coder golden
// cases provide plan + gathered_context rather than a design document,
// so the design input is composed from both.
func (a *adapters) runCoder(tc eval.TestCase, _ int) (string, []eval.FileState, error) {
	ctx, cancel := a.runCtx()
	defer cancel()
	in := tc.Inputs
	design := in["plan"]
	if gc := in["gathered_context"]; gc != "" {
		design += "\n\n## Existing Code Context\n\n" + gc
	}
	res, err := agents.CodeWithUsage(ctx, a.agentClient, a.agentModel,
		design, in["research_context"], in["conventions"], nil, nil)
	if err != nil {
		return "", nil, err
	}
	a.addUsage(res.PromptTokens, res.CompTokens)
	content := eval.StripReasoning(res.Content)
	parsed, err := pipeline.ParseStructuredCodeOutput(content)
	if err != nil {
		parsed = pipeline.ParseCodeOutput(content)
	}
	evalFiles := make([]eval.FileState, 0, len(parsed))
	for _, f := range parsed {
		evalFiles = append(evalFiles, eval.FileState{Path: f.Path})
	}
	return content, evalFiles, nil
}

// caseFinding matches the JSON shape used by arbiter golden cases.
type caseFinding struct {
	Category    string `json:"category"`
	Description string `json:"description"`
	File        string `json:"file"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
}

func parseCaseFindings(raw, source string) ([]review.ReviewFinding, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var cfs []caseFinding
	if err := json.Unmarshal([]byte(raw), &cfs); err != nil {
		return nil, fmt.Errorf("parse %s findings: %w", source, err)
	}
	findings := make([]review.ReviewFinding, 0, len(cfs))
	for _, cf := range cfs {
		title := cf.Title
		if title == "" {
			title = cf.Description
		}
		sev := cf.Severity
		if sev == "" {
			sev = "medium"
		}
		findings = append(findings, review.ReviewFinding{
			Source:   source,
			Severity: review.Severity(sev),
			Category: review.Category(cf.Category),
			Title:    title,
			Detail:   cf.Description,
			File:     cf.File,
		})
	}
	return findings, nil
}

func (a *adapters) runArbiter(tc eval.TestCase, _ int) (string, []eval.FileState, error) {
	ctx, cancel := a.runCtx()
	defer cancel()
	in := tc.Inputs

	qodo, err := parseCaseFindings(in["qodo_findings"], "qodo")
	if err != nil {
		return "", nil, err
	}
	factory, err := parseCaseFindings(in["factory_findings"], "factory")
	if err != nil {
		return "", nil, err
	}
	findings := append(qodo, factory...)

	res, err := agents.Arbitrate(ctx, a.arbClient, a.arbModel,
		findings, in["conventions"], in["architecture"], in["plan"], nil)
	if err != nil {
		return "", nil, err
	}
	a.addUsage(res.PromptTokens, res.CompTokens)

	var b strings.Builder
	for _, f := range res.Result.Findings {
		fmt.Fprintf(&b, "%s: %s — %s\n", f.Classification, f.Finding.Title, f.Reason)
	}
	return b.String(), nil, nil
}
