package openshell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestSandboxName(t *testing.T) {
	tests := []struct {
		phase string
		issue int
		want  string
	}{
		{"coder", 42, "factory-coder-42"},
		{"gatherer", 1, "factory-gatherer-1"},
		{"researcher", 999, "factory-researcher-999"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := SandboxName(tt.phase, tt.issue)
			if got != tt.want {
				t.Errorf("SandboxName(%q, %d) = %q, want %q", tt.phase, tt.issue, got, tt.want)
			}
		})
	}
}

func TestSandboxConfig_Validate(t *testing.T) {
	valid := SandboxConfig{
		Name:       "factory-coder-42",
		Image:      "factory-go:latest",
		PolicyPath: "/etc/policies/coder.yaml",
		Binary:     "coder",
		StatePath:  "/data/pipeline/owner/repo/42.json",
	}

	t.Run("valid config", func(t *testing.T) {
		if err := valid.Validate(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	fields := []struct {
		name  string
		unset func(SandboxConfig) SandboxConfig
	}{
		{"name", func(c SandboxConfig) SandboxConfig { c.Name = ""; return c }},
		{"image", func(c SandboxConfig) SandboxConfig { c.Image = ""; return c }},
		{"policy path", func(c SandboxConfig) SandboxConfig { c.PolicyPath = ""; return c }},
		{"binary", func(c SandboxConfig) SandboxConfig { c.Binary = ""; return c }},
		{"state path", func(c SandboxConfig) SandboxConfig { c.StatePath = ""; return c }},
	}
	for _, f := range fields {
		t.Run("missing "+f.name, func(t *testing.T) {
			cfg := f.unset(valid)
			if err := cfg.Validate(); err == nil {
				t.Errorf("expected error for missing %s", f.name)
			}
		})
	}
}

func TestRunInSandbox_CommandSequence(t *testing.T) {
	var commands []string
	origRunner := cmdRunner
	cmdRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		full := strings.Join(append([]string{name}, args...), " ")
		commands = append(commands, full)
		return exec.CommandContext(ctx, "true")
	}
	defer func() { cmdRunner = origRunner }()

	cfg := SandboxConfig{
		Name:       "factory-coder-42",
		Image:      "factory-go:latest",
		PolicyPath: "/policies/coder.yaml",
		Binary:     "coder",
		StatePath:  "/data/state.json",
		Env:        []string{"PIPELINE_STATE_PATH=/work/state.json", "CONFIG_PATH=/work/config.json"},
	}

	err := RunInSandbox(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"openshell sandbox create --name factory-coder-42 --image factory-go:latest --policy /policies/coder.yaml",
		"openshell sandbox cp /data/state.json factory-coder-42:/work/state.json",
		"openshell sandbox exec --env PIPELINE_STATE_PATH=/work/state.json --env CONFIG_PATH=/work/config.json factory-coder-42 -- /usr/local/bin/coder",
		"openshell sandbox cp factory-coder-42:/work/state.json /data/state.json",
		"openshell sandbox rm factory-coder-42",
	}

	if len(commands) != len(expected) {
		t.Fatalf("got %d commands, want %d:\n%s", len(commands), len(expected), strings.Join(commands, "\n"))
	}
	for i, want := range expected {
		if commands[i] != want {
			t.Errorf("command[%d]:\n  got:  %s\n  want: %s", i, commands[i], want)
		}
	}
}

func TestRunInSandbox_DestroyOnExecFailure(t *testing.T) {
	var commands []string
	origRunner := cmdRunner
	cmdRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		full := strings.Join(append([]string{name}, args...), " ")
		commands = append(commands, full)
		if strings.Contains(full, "sandbox exec") {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "true")
	}
	defer func() { cmdRunner = origRunner }()

	cfg := SandboxConfig{
		Name:       "factory-coder-99",
		Image:      "factory-go:latest",
		PolicyPath: "/policies/coder.yaml",
		Binary:     "coder",
		StatePath:  "/data/state.json",
	}

	err := RunInSandbox(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error from exec failure")
	}

	last := commands[len(commands)-1]
	if !strings.Contains(last, "sandbox rm") {
		t.Errorf("sandbox was not destroyed after exec failure; last command: %s", last)
	}
}

func TestRunInSandbox_ValidationError(t *testing.T) {
	cfg := SandboxConfig{}
	err := RunInSandbox(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "sandbox config") {
		t.Errorf("error should mention sandbox config: %v", err)
	}
}

func TestRunInSandbox_CreateFailure(t *testing.T) {
	var commands []string
	origRunner := cmdRunner
	cmdRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		full := strings.Join(append([]string{name}, args...), " ")
		commands = append(commands, full)
		if strings.Contains(full, "sandbox create") {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "true")
	}
	defer func() { cmdRunner = origRunner }()

	cfg := SandboxConfig{
		Name:       "factory-coder-1",
		Image:      "factory-go:latest",
		PolicyPath: "/policies/coder.yaml",
		Binary:     "coder",
		StatePath:  "/data/state.json",
	}

	err := RunInSandbox(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error from create failure")
	}

	if len(commands) != 1 {
		t.Errorf("expected only 1 command (create), got %d: %s", len(commands), strings.Join(commands, "\n"))
	}

	for _, cmd := range commands {
		if strings.Contains(cmd, "sandbox rm") {
			t.Error("sandbox rm should not be called when create fails")
		}
	}
}

func TestRunInSandbox_NoEnvVars(t *testing.T) {
	var commands []string
	origRunner := cmdRunner
	cmdRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		full := strings.Join(append([]string{name}, args...), " ")
		commands = append(commands, full)
		return exec.CommandContext(ctx, "true")
	}
	defer func() { cmdRunner = origRunner }()

	cfg := SandboxConfig{
		Name:       "factory-coder-1",
		Image:      "factory-go:latest",
		PolicyPath: "/policies/coder.yaml",
		Binary:     "coder",
		StatePath:  "/data/state.json",
	}

	if err := RunInSandbox(context.Background(), cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	execCmd := ""
	for _, c := range commands {
		if strings.Contains(c, "sandbox exec") {
			execCmd = c
			break
		}
	}
	if execCmd == "" {
		t.Fatal("no exec command found")
	}
	wantExec := fmt.Sprintf("openshell sandbox exec %s -- /usr/local/bin/coder", cfg.Name)
	if execCmd != wantExec {
		t.Errorf("exec command:\n  got:  %s\n  want: %s", execCmd, wantExec)
	}
}
