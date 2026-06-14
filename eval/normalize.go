package eval

import "strings"

// StripReasoning removes <think>...</think> blocks emitted by reasoning
// models (qwen3.x, gemma4) so assertions check the final answer rather
// than the deliberation. An unclosed block is stripped to end of input.
func StripReasoning(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			return strings.TrimSpace(s)
		}
		rest := s[start+len("<think>"):]
		end := strings.Index(rest, "</think>")
		if end < 0 {
			return strings.TrimSpace(s[:start])
		}
		s = s[:start] + rest[end+len("</think>"):]
	}
}
