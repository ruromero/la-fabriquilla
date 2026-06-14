package eval

import "testing"

func TestStripReasoning(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"no thinking", "PLAN:\n1. do x", "PLAN:\n1. do x"},
		{"think block stripped", "<think>hmm let me reason</think>\nPLAN:\n1. do x", "PLAN:\n1. do x"},
		{"multiple blocks", "<think>a</think>one<think>b</think> two", "one two"},
		{"unclosed block drops tail", "answer<think>never closed", "answer"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripReasoning(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
