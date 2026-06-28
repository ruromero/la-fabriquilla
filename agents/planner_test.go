package agents

import "testing"

func TestBuildPlannerPrompt_WithReplanFeedback(t *testing.T) {
	prompt := buildPlannerPrompt("Fix bug", "The login is broken", "", "gathered ctx", "conventions", "", "Function Foo does not exist")
	if !contains(prompt, "Re-plan Feedback") {
		t.Error("prompt should contain Re-plan Feedback section")
	}
	if !contains(prompt, "Function Foo does not exist") {
		t.Error("prompt should contain the infeasibility reason")
	}
}

func TestBuildPlannerPrompt_WithoutReplanFeedback(t *testing.T) {
	prompt := buildPlannerPrompt("Fix bug", "The login is broken", "", "gathered ctx", "conventions", "", "")
	if contains(prompt, "Re-plan Feedback") {
		t.Error("prompt should not contain Re-plan Feedback when feedback is empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
