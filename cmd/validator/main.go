package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
)

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	repoCfg, ok := helpers.FindRepoConfig(cfg, state.RepoOwner, state.RepoName)
	if !ok || len(repoCfg.ValidateCommands) == 0 {
		slog.Info("no validate commands configured, skipping")
		state.ValidatePass = true
		state.Phase = "validate-done"
		helpers.MustSaveState(state)
		return
	}

	gh := helpers.MustGitHubClientForApp(cfg, "worker", state)
	ctx := context.Background()

	slog.Info("cloning repository", "repo", state.RepoOwner+"/"+state.RepoName)
	cloneDir, cleanup, err := gh.CloneShallow(ctx)
	if err != nil {
		slog.Error("failed to clone repository", "error", err)
		state.ValidatePass = false
		state.ValidateOutput = fmt.Sprintf("clone failed: %v", err)
		state.Phase = "validate-done"
		helpers.MustSaveState(state)
		return
	}
	defer cleanup()

	slog.Info("applying generated files", "count", len(state.Files))
	for _, f := range state.Files {
		target := filepath.Join(cloneDir, f.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			slog.Error("failed to create directory", "path", f.Path, "error", err)
			state.ValidatePass = false
			state.ValidateOutput = fmt.Sprintf("mkdir %s: %v", filepath.Dir(f.Path), err)
			state.Phase = "validate-done"
			helpers.MustSaveState(state)
			return
		}
		if err := os.WriteFile(target, []byte(f.Content), 0o644); err != nil {
			slog.Error("failed to write file", "path", f.Path, "error", err)
			state.ValidatePass = false
			state.ValidateOutput = fmt.Sprintf("write %s: %v", f.Path, err)
			state.Phase = "validate-done"
			helpers.MustSaveState(state)
			return
		}
	}

	var output strings.Builder
	allPassed := true

	for _, cmdStr := range repoCfg.ValidateCommands {
		slog.Info("running validate command", "cmd", cmdStr)
		parts := strings.Fields(cmdStr)
		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		cmd.Dir = cloneDir

		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		if err := cmd.Run(); err != nil {
			slog.Warn("validate command failed", "cmd", cmdStr, "error", err)
			fmt.Fprintf(&output, "FAIL: %s\n%s\n\n", cmdStr, buf.String())
			allPassed = false
			break
		}
		slog.Info("validate command passed", "cmd", cmdStr)
		fmt.Fprintf(&output, "PASS: %s\n", cmdStr)
	}

	state.ValidatePass = allPassed
	if !allPassed {
		state.ValidateOutput = output.String()
	} else {
		state.ValidateOutput = ""
	}
	state.Phase = "validate-done"
	helpers.MustSaveState(state)

	if allPassed {
		slog.Info("all validation commands passed")
	} else {
		slog.Warn("validation failed", "output_len", len(state.ValidateOutput))
	}
}
