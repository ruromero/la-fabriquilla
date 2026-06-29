package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/openshell"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/review"
)

var configPath string

type rateTracker struct {
	hourly []time.Time
	daily  []time.Time
}

func (r *rateTracker) record() {
	now := time.Now()
	r.hourly = append(r.hourly, now)
	r.daily = append(r.daily, now)
}

func (r *rateTracker) pruneAndCheck(maxPerHour, maxPerDay int) error {
	now := time.Now()
	hourAgo := now.Add(-1 * time.Hour)
	dayAgo := now.Add(-24 * time.Hour)

	// Prune old entries
	r.hourly = pruneOlderThan(r.hourly, hourAgo)
	r.daily = pruneOlderThan(r.daily, dayAgo)

	if maxPerHour > 0 && len(r.hourly) >= maxPerHour {
		return fmt.Errorf("hourly rate limit reached: %d/%d issues in the last hour", len(r.hourly), maxPerHour)
	}
	if maxPerDay > 0 && len(r.daily) >= maxPerDay {
		return fmt.Errorf("daily rate limit reached: %d/%d issues today", len(r.daily), maxPerDay)
	}
	return nil
}

func pruneOlderThan(times []time.Time, cutoff time.Time) []time.Time {
	var result []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			result = append(result, t)
		}
	}
	return result
}

var rates rateTracker

func main() {
	flag.StringVar(&configPath, "config", "config.json", "path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.ValidateRepos(); err != nil {
		slog.Error("invalid repo config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	repoNames := make([]string, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repoNames[i] = r.Owner + "/" + r.Repo
	}
	slog.Info("factory orchestrator starting",
		"repos", repoNames,
		"poll_interval", cfg.PollInterval.String(),
		"shadow_mode", cfg.ShadowMode,
	)

	ticker := time.NewTicker(cfg.PollInterval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pollAllRepos(ctx, cfg)
		case <-sigCh:
			slog.Info("shutting down")
			cancel()
			return
		case <-ctx.Done():
			return
		}
	}
}

func pollAllRepos(ctx context.Context, cfg config.Config) {
	for _, repo := range cfg.Repos {
		gh, err := helpers.NewGitHubClientForApp(cfg, "dispatcher", repo.Owner, repo.Repo)
		if err != nil {
			slog.Error("failed to create github client", "repo", repo.Owner+"/"+repo.Repo, "error", err)
			continue
		}
		log := slog.With("repo", repo.Owner+"/"+repo.Repo)

		readiness, err := gh.CheckReadiness(ctx)
		if err != nil {
			log.Error("readiness check failed", "error", err)
			continue
		}
		if !readiness.Ready {
			log.Warn("repo not ready", "missing", readiness.Missing)
			notifyReadinessFailure(ctx, gh, readiness)
			continue
		}

		issues, err := gh.ListIssuesByLabel(ctx, "fabriquilla:ready")
		if err != nil {
			log.Error("failed to poll issues", "error", err)
			continue
		}

		for _, issue := range issues {
			log.Info("processing issue", "number", issue.Number, "title", issue.Title)

			if err := rates.pruneAndCheck(cfg.MaxIssuesPerHour, cfg.MaxIssuesPerDay); err != nil {
				log.Warn("rate limit exceeded, skipping remaining issues", "error", err)
				break
			}

			if err := gh.AddLabel(ctx, issue.Number, "fabriquilla:in-progress"); err != nil {
				log.Error("failed to add label", "issue", issue.Number, "error", err)
				continue
			}
			if err := gh.RemoveLabel(ctx, issue.Number, "fabriquilla:ready"); err != nil {
				log.Error("failed to remove label", "issue", issue.Number, "error", err)
			}

			if err := processIssue(ctx, gh, cfg, repo, issue); err != nil {
				log.Error("failed to process issue", "issue", issue.Number, "error", err)
				gh.AddLabel(ctx, issue.Number, "fabriquilla:needs-human")
			} else {
				rates.record()
			}
		}
	}
}

func selectSandboxImage(globalImage, repoImage string) string {
	if repoImage != "" {
		return repoImage
	}
	return globalImage
}

func repoSandboxImage(cfg config.Config, owner, repo string) string {
	for _, r := range cfg.Repos {
		if r.Owner == owner && r.Repo == repo {
			if r.SandboxImage != "" {
				return r.SandboxImage
			}
			if r.Language != "" {
				return "factory-" + r.Language + ":latest"
			}
			return ""
		}
	}
	return ""
}

func processIssue(ctx context.Context, gh *github.Client, cfg config.Config, repo config.RepoConfig, issue github.Issue) error {
	store := pipeline.NewFileStateStore(cfg.StateDir)
	sandboxImage := repoSandboxImage(cfg, gh.Owner(), gh.Repo())

	orch := &pipeline.Orchestrator{
		GH:           gh,
		Config:       &cfg,
		Store:        store,
		RunPhase:     pipeline.PhaseRunner(runPhase),
		RunArbiter:   buildArbiterFunc(&cfg),
		SandboxImage: sandboxImage,
		ConfigPath:   configPath,
		IncludeDocs:  repo.EffectiveIncludeDocs(),
	}

	_, err := orch.ProcessIssue(ctx, issue)
	return err
}

func buildArbiterFunc(cfg *config.Config) pipeline.ArbiterFunc {
	if cfg.Arbiter.Model == "" {
		return nil
	}
	return func(ctx context.Context, findings []review.ReviewFinding, conventions, architecture, plan string, dismissedKeys []string) (review.ArbiterResult, error) {
		model, baseURL, apiKey, err := cfg.ResolveModel(cfg.Arbiter.Model)
		if err != nil {
			return review.ArbiterResult{}, fmt.Errorf("resolve arbiter model: %w", err)
		}
		cl := inference.NewClient(baseURL, inference.WithAPIKey(apiKey))
		out, err := agents.Arbitrate(ctx, cl, model, findings, conventions, architecture, plan, dismissedKeys)
		if err != nil {
			return review.ArbiterResult{}, err
		}
		return out.Result, nil
	}
}

// noRetryPhases lists phases that must not be retried because they
// have non-idempotent side effects (creating branches, PRs).
var noRetryPhases = map[string]bool{
	"committer": true,
	"iterator":  true,
}

// sandboxMVPPhases lists phases enabled for sandbox execution in the MVP.
// Expand this set as sandbox images and policies are validated per phase.
var sandboxMVPPhases = map[string]bool{
	"coder":     true,
	"iterator":  true,
	"validator": true,
}

const maxBackoff = 2 * time.Minute

func runPhase(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, repoImage string) error {
	maxRetries := cfg.MaxPhaseRetries
	if maxRetries < 0 {
		maxRetries = 2
	}
	if noRetryPhases[binary] {
		maxRetries = 0
	}
	timeout := cfg.PhaseDuration(binary)
	useSandbox := cfg.Sandbox.Enabled && sandboxMVPPhases[binary]

	slog.Info("running phase", "phase", binary, "sandboxed", useSandbox)

	var sbxCfg openshell.SandboxConfig
	if useSandbox {
		sbxCfg = openshell.SandboxConfig{
			Name:       openshell.SandboxName(binary, issueNumber),
			Image:      selectSandboxImage(cfg.Sandbox.Image, repoImage),
			PolicyPath: fmt.Sprintf("%s/%s.yaml", cfg.Sandbox.PolicyDir, binary),
			Binary:     binary,
			StatePath:  statePath,
			ConfigPath: configPath,
			Env: []string{
				"PIPELINE_STATE_PATH=/work/state.json",
				"CONFIG_PATH=/work/config.json",
			},
		}
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(30<<(attempt-1)) * time.Second
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			slog.Info("retrying phase", "phase", binary, "attempt", attempt+1, "backoff", backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		phaseCtx, cancel := context.WithTimeout(ctx, timeout)

		var err error
		sandboxed := useSandbox
		if sandboxed {
			err = openshell.RunInSandbox(phaseCtx, sbxCfg)
			if errors.Is(err, openshell.ErrUnavailable) {
				slog.Warn("openshell not available, falling back to subprocess", "phase", binary)
				sandboxed = false
				err = nil
			}
		}
		if !sandboxed {
			cmd := exec.CommandContext(phaseCtx, binary)
			cmd.Env = append(os.Environ(),
				"PIPELINE_STATE_PATH="+statePath,
				"CONFIG_PATH="+configPath,
			)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
		}

		// Check phaseCtx before cancel — exec.CommandContext sends SIGKILL
		// on deadline, but the resulting ExitError doesn't wrap DeadlineExceeded.
		timedOut := phaseCtx.Err() == context.DeadlineExceeded
		cancel()

		if err == nil {
			return nil
		}

		lastErr = fmt.Errorf("%s (attempt %d/%d): %w", binary, attempt+1, maxRetries+1, err)
		slog.Warn("phase failed", "phase", binary, "attempt", attempt+1, "error", err, "timed_out", timedOut)

		if !timedOut && !isRetryable(err) {
			return lastErr
		}
	}
	return lastErr
}

func isRetryable(err error) bool {
	// Timeout (hung phase) — retryable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Signal kill (OOM, etc.) — retryable
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() > 128 // killed by signal
	}
	// Normal exit code (1, 2, etc.) — permanent failure
	return false
}

const readinessCommentMarker = "<!-- fabriquilla:readiness -->"

func notifyReadinessFailure(ctx context.Context, gh *github.Client, readiness github.ReadinessResult) {
	issues, err := gh.ListIssuesByLabel(ctx, "fabriquilla:ready")
	if err != nil || len(issues) == 0 {
		return
	}

	comment := fmt.Sprintf("%s\n## Factory: Repository Not Ready\n\nThis repository is missing required files:\n\n", readinessCommentMarker)
	for _, f := range readiness.Missing {
		comment += fmt.Sprintf("- `%s`\n", f)
	}
	comment += "\nSee [Repo readiness](https://github.com/ruromero/la-fabriquilla#repo-readiness) for details on required files.\n"
	comment += "Once the missing files are added, relabel this issue `fabriquilla:ready` to retry."

	for _, issue := range issues {
		existing, err := gh.ListComments(ctx, issue.Number)
		if err != nil {
			continue
		}
		alreadyNotified := false
		for _, c := range existing {
			if strings.Contains(c.Body, readinessCommentMarker) {
				alreadyNotified = true
				break
			}
		}
		if alreadyNotified {
			continue
		}

		if err := gh.CreateComment(ctx, issue.Number, comment); err != nil {
			slog.Error("failed to post readiness comment", "issue", issue.Number, "error", err)
			continue
		}
		gh.RemoveLabel(ctx, issue.Number, "fabriquilla:ready")
		gh.AddLabel(ctx, issue.Number, "fabriquilla:requirements")
		slog.Info("notified issue about missing requirements", "issue", issue.Number, "missing", readiness.Missing)
	}
}
