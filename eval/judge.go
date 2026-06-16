package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// JudgeFunc calls a frontier LLM to evaluate agent output against a
// directive. It returns pass/fail, a short explanation, and any error.
type JudgeFunc func(ctx context.Context, system, user string) (pass bool, explanation string, err error)

// JudgeResponse is the structured JSON response expected from the judge model.
type JudgeResponse struct {
	Pass        bool   `json:"pass"`
	Explanation string `json:"explanation"`
}

// ParseJudgeResponse extracts a JudgeResponse from the model's output.
// It tries to find a JSON object in the text, tolerating markdown fences.
func ParseJudgeResponse(raw string) (JudgeResponse, error) {
	s := strings.TrimSpace(raw)
	if start := strings.Index(s, "```json"); start >= 0 {
		s = s[start+len("```json"):]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if start := strings.Index(s, "```"); start >= 0 {
		s = s[start+len("```"):]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	s = strings.TrimSpace(s)

	// Find the JSON object boundaries.
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return JudgeResponse{}, fmt.Errorf("no JSON object found in judge response")
	}
	s = s[start : end+1]

	var resp JudgeResponse
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		return JudgeResponse{}, fmt.Errorf("parse judge response: %w", err)
	}
	return resp, nil
}

// BuildJudgePrompt constructs the system and user prompts for the judge model.
func BuildJudgePrompt(tc TestCase, output, directive string) (system, user string) {
	system = `You are a strict evaluator grading an AI agent's output.

Rules:
- ALL criteria in the directive must be met for "pass" to be true.
- Be strict: "close enough" or "partially addresses" is FAIL.
- Evaluate what was actually produced, not what was intended.
- If ANY criterion fails, the overall verdict is FAIL.

Respond with ONLY a JSON object, no other text:
{"pass": true, "explanation": "one sentence per criterion"}
or
{"pass": false, "explanation": "which criteria failed and why"}`

	var b strings.Builder
	b.WriteString("## Task Given to the Agent\n\n")
	for k, v := range tc.Inputs {
		fmt.Fprintf(&b, "**%s:** %s\n\n", k, v)
	}
	b.WriteString("## Agent Output\n\n")
	b.WriteString(output)
	b.WriteString("\n\n## Evaluation Criteria\n\n")
	b.WriteString(directive)

	return system, b.String()
}
