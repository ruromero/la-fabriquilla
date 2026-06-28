package agents

import "testing"

func TestParseCoderOutput_PlanInfeasible(t *testing.T) {
	content := "PLAN_INFEASIBLE: Function Foo does not exist in pkg/bar.go. The design assumes a helper that was never implemented."
	outcome, reason := ParseCoderOutput(content)
	if outcome != "plan_infeasible" {
		t.Errorf("outcome = %q, want %q", outcome, "plan_infeasible")
	}
	want := "Function Foo does not exist in pkg/bar.go. The design assumes a helper that was never implemented."
	if reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
}

func TestParseCoderOutput_Success(t *testing.T) {
	content := `{"files": [{"path": "main.go", "language": "go", "content": "package main"}]}`
	outcome, reason := ParseCoderOutput(content)
	if outcome != "success" {
		t.Errorf("outcome = %q, want %q", outcome, "success")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

func TestParseCoderOutput_PlanInfeasibleWithWhitespace(t *testing.T) {
	content := "PLAN_INFEASIBLE:   needs rework  \n"
	outcome, reason := ParseCoderOutput(content)
	if outcome != "plan_infeasible" {
		t.Errorf("outcome = %q, want %q", outcome, "plan_infeasible")
	}
	if reason != "needs rework" {
		t.Errorf("reason = %q, want %q", reason, "needs rework")
	}
}

func TestParseCoderOutput_PrefixOnly(t *testing.T) {
	content := "PLAN_INFEASIBLE:"
	outcome, reason := ParseCoderOutput(content)
	if outcome != "plan_infeasible" {
		t.Errorf("outcome = %q, want %q", outcome, "plan_infeasible")
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}
