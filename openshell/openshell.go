package openshell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrUnavailable is returned when the openshell CLI is not installed.
// Callers should fall back to direct subprocess execution.
var ErrUnavailable = errors.New("openshell CLI not found in PATH")

// SandboxConfig holds the parameters for running a phase binary inside
// an OpenShell sandbox.
type SandboxConfig struct {
	Name       string
	Image      string
	PolicyPath string
	Binary     string
	StatePath  string
	ConfigPath string
	Env        []string
}

// Validate checks that all required fields are populated.
func (c SandboxConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("sandbox name is required")
	}
	if c.Image == "" {
		return fmt.Errorf("sandbox image is required")
	}
	if c.PolicyPath == "" {
		return fmt.Errorf("sandbox policy path is required")
	}
	if c.Binary == "" {
		return fmt.Errorf("sandbox binary is required")
	}
	if c.StatePath == "" {
		return fmt.Errorf("sandbox state path is required")
	}
	if c.ConfigPath == "" {
		return fmt.Errorf("sandbox config path is required")
	}
	return nil
}

// SandboxName returns a deterministic sandbox name for a phase and issue.
func SandboxName(phase string, issueNumber int) string {
	return fmt.Sprintf("factory-%s-%d", phase, issueNumber)
}

// cmdRunner abstracts exec.CommandContext for testing.
var cmdRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// lookPath abstracts exec.LookPath for testing.
var lookPath = exec.LookPath

// RunInSandbox executes a phase binary inside an OpenShell sandbox.
// The lifecycle is: create → upload state → exec binary → download state → destroy.
// The sandbox is always destroyed on exit, even if an earlier step fails.
func RunInSandbox(ctx context.Context, cfg SandboxConfig) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("sandbox config: %w", err)
	}

	if _, err := lookPath("openshell"); err != nil {
		return ErrUnavailable
	}

	log := slog.With("sandbox", cfg.Name, "phase", cfg.Binary)

	// Best-effort cleanup of any leftover sandbox from a previous failed attempt.
	_ = run(ctx, log, "openshell", "sandbox", "rm", cfg.Name)

	log.Info("creating sandbox")
	if err := run(ctx, log, "openshell", "sandbox", "create",
		"--name", cfg.Name,
		"--image", cfg.Image,
		"--policy", cfg.PolicyPath,
	); err != nil {
		return fmt.Errorf("create sandbox %s: %w", cfg.Name, err)
	}

	defer func() {
		log.Info("destroying sandbox")
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rmCancel()
		if err := run(rmCtx, log, "openshell", "sandbox", "rm", cfg.Name); err != nil {
			log.Error("failed to destroy sandbox", "error", err)
		}
	}()

	log.Info("uploading state")
	remoteState := cfg.Name + ":/work/state.json"
	if err := run(ctx, log, "openshell", "sandbox", "cp", cfg.StatePath, remoteState); err != nil {
		return fmt.Errorf("upload state to %s: %w", cfg.Name, err)
	}

	log.Info("uploading config")
	remoteConfig := cfg.Name + ":/work/config.json"
	if err := run(ctx, log, "openshell", "sandbox", "cp", cfg.ConfigPath, remoteConfig); err != nil {
		return fmt.Errorf("upload config to %s: %w", cfg.Name, err)
	}

	log.Info("executing phase binary")
	execArgs := []string{"sandbox", "exec"}
	for _, e := range cfg.Env {
		execArgs = append(execArgs, "--env", e)
	}
	execArgs = append(execArgs, cfg.Name, "--", "/usr/local/bin/"+cfg.Binary)
	if err := run(ctx, log, "openshell", execArgs...); err != nil {
		return fmt.Errorf("exec %s in sandbox %s: %w", cfg.Binary, cfg.Name, err)
	}

	log.Info("downloading state")
	if err := run(ctx, log, "openshell", "sandbox", "cp", remoteState, cfg.StatePath); err != nil {
		return fmt.Errorf("download state from %s: %w", cfg.Name, err)
	}

	return nil
}

func run(ctx context.Context, log *slog.Logger, name string, args ...string) error {
	cmd := cmdRunner(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Debug("running command", "cmd", strings.Join(append([]string{name}, args...), " "))
	return cmd.Run()
}
