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

	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/harness"
	"github.com/ruromero/la-fabriquilla/openshell"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/sandbox"
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

			if err := processIssue(ctx, gh, cfg, issue); err != nil {
				log.Error("failed to process issue", "issue", issue.Number, "error", err)
				gh.AddLabel(ctx, issue.Number, "fabriquilla:needs-human")
			} else {
				rates.record()
			}
		}
	}
}

func processIssue(ctx context.Context, gh *github.Client, cfg config.Config, issue github.Issue) error {
	log := slog.With("issue", issue.Number)

	store := pipeline.NewFileStateStore(cfg.StateDir)
	key := pipeline.StateKey(gh.Owner(), gh.Repo(), issue.Number)

	rc := harness.LoadRepoContext(ctx, gh)

	issueTitle := sandbox.SanitizeInput(issue.Title)
	issueBody := sandbox.SanitizeInput(issue.Body)
	commentHistory := loadHumanComments(ctx, gh, issue.Number)

	state := &pipeline.State{
		RepoOwner:      gh.Owner(),
		RepoName:       gh.Repo(),
		IssueNumber:    issue.Number,
		Phase:          "init",
		IssueTitle:     issueTitle,
		IssueBody:      issueBody,
		CommentHistory: commentHistory,
		Summaries:      rc.Summaries(),
		Conventions:    rc.Conventions(),
		StartedAt:      time.Now(),
	}

	sess, err := harness.CloneAndStartSerena(ctx, gh, cfg.Serena)
	if err != nil {
		log.Warn("failed to start Serena, continuing without", "error", err)
	}
	if sess != nil {
		defer sess.Cleanup()
		state.CloneDir = sess.CloneDir
	}

	if err := store.Save(ctx, key, state); err != nil {
		return fmt.Errorf("save initial state: %w", err)
	}

	statePath := store.StatePath(key)

	log.Info("starting gather phase")
	if err := runPhase(ctx, &cfg, "gatherer", statePath, issue.Number); err != nil {
		return fmt.Errorf("gather phase: %w", err)
	}
	state, err = store.Load(ctx, key)
	if err != nil {
		return fmt.Errorf("reload state after gather: %w", err)
	}
	if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
		return fmt.Errorf("budget exceeded after gather: %w", err)
	}

	log.Info("starting research phase")
	if err := runPhase(ctx, &cfg, "researcher", statePath, issue.Number); err != nil {
		log.Warn("research phase failed, continuing", "error", err)
	} else {
		state, err = store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after research: %w", err)
		}
		if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after research: %w", err)
		}
	}

	log.Info("starting plan phase")
	if err := runPhase(ctx, &cfg, "planner", statePath, issue.Number); err != nil {
		return fmt.Errorf("plan phase: %w", err)
	}

	state, err = store.Load(ctx, key)
	if err != nil {
		return fmt.Errorf("reload state after plan: %w", err)
	}
	if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
		return fmt.Errorf("budget exceeded after plan: %w", err)
	}

	switch state.PlanOutcome {
	case "needs_info":
		log.Info("planner needs more info")
		comment := fmt.Sprintf("## Factory: Additional Information Needed\n\n%s", state.PlanContent)
		if err := gh.CreateComment(ctx, issue.Number, comment); err != nil {
			return fmt.Errorf("post needs-info comment: %w", err)
		}
		gh.RemoveLabel(ctx, issue.Number, "fabriquilla:in-progress")
		return gh.AddLabel(ctx, issue.Number, "fabriquilla:needs-info")

	case "decompose":
		log.Info("planner decomposing issue")
		comment := fmt.Sprintf("## Factory: Issue Decomposed\n\nThis issue is too complex for a single PR. Creating sub-issues.\n\n%s", state.PlanContent)
		if err := gh.CreateComment(ctx, issue.Number, comment); err != nil {
			return fmt.Errorf("post decompose comment: %w", err)
		}
		if err := createSubIssues(ctx, gh, issue.Number, state.PlanContent); err != nil {
			return fmt.Errorf("create sub-issues: %w", err)
		}
		gh.RemoveLabel(ctx, issue.Number, "fabriquilla:in-progress")
		return gh.AddLabel(ctx, issue.Number, "fabriquilla:tracking")

	case "plan":
		log.Info("plan produced, posting to issue")
		comment := fmt.Sprintf("## Factory: Implementation Plan\n\n%s", state.PlanContent)
		if state.ResearchContext != "" {
			comment += fmt.Sprintf("\n\n<details><summary>Research Context</summary>\n\n%s\n\n</details>", state.ResearchContext)
		}
		if err := gh.CreateComment(ctx, issue.Number, comment); err != nil {
			return fmt.Errorf("post plan comment: %w", err)
		}

		log.Info("starting design phase")
		if err := runPhase(ctx, &cfg, "designer", statePath, issue.Number); err != nil {
			return fmt.Errorf("design phase: %w", err)
		}
		state, err = store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after design: %w", err)
		}
		if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after design: %w", err)
		}

		log.Info("starting code phase (includes review+iterate)")
		if err := runPhase(ctx, &cfg, "coder", statePath, issue.Number); err != nil {
			return fmt.Errorf("code phase: %w", err)
		}
		state, err = store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after code: %w", err)
		}
		if err := pipeline.CheckCostBudget(state, cfg.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after code: %w", err)
		}
		if err := pipeline.CheckPRScope(state.Files, cfg.MaxFilesChanged, cfg.MaxPRSizeLines); err != nil {
			return fmt.Errorf("scope check: %w", err)
		}
		if err := pipeline.ValidateFiles(state.Files, cfg.BlockedPaths); err != nil {
			return fmt.Errorf("path validation: %w", err)
		}
		if violations := pipeline.ValidateContents(state.Files); len(violations) > 0 {
			return fmt.Errorf("secret detected in generated code: %s in %s line %d", violations[0].Pattern, violations[0].File, violations[0].Line)
		}

		log.Info("starting commit phase")
		if err := runPhase(ctx, &cfg, "committer", statePath, issue.Number); err != nil {
			return fmt.Errorf("commit phase: %w", err)
		}

		state, err = store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after commit: %w", err)
		}

		if state.PRNumber > 0 {
			log.Info("PR created", "pr", state.PRNumber)
		}
		return nil
	}

	return fmt.Errorf("unknown plan outcome: %s", state.PlanOutcome)
}

// noRetryPhases lists phases that must not be retried because they
// have non-idempotent side effects (creating branches, PRs).
var noRetryPhases = map[string]bool{
	"committer": true,
}

// sandboxMVPPhases lists phases enabled for sandbox execution in the MVP.
// Expand this set as sandbox images and policies are validated per phase.
var sandboxMVPPhases = map[string]bool{
	"coder": true,
}

const maxBackoff = 2 * time.Minute

func runPhase(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int) error {
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
			Image:      cfg.Sandbox.Image,
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

func loadHumanComments(ctx context.Context, gh *github.Client, issueNumber int) string {
	comments, err := gh.ListComments(ctx, issueNumber)
	if err != nil {
		slog.Warn("could not load issue comments", "issue", issueNumber, "error", err)
		return ""
	}

	var b strings.Builder
	for _, c := range comments {
		if strings.HasSuffix(c.User.Login, "[bot]") {
			continue
		}
		body := sandbox.SanitizeInput(c.Body)
		if body == "" {
			continue
		}
		fmt.Fprintf(&b, "**@%s**:\n%s\n\n", c.User.Login, body)
	}
	return strings.TrimSpace(b.String())
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

func createSubIssues(ctx context.Context, gh *github.Client, parentNumber int, decomposeContent string) error {
	lines := strings.Split(decomposeContent, "\n")
	var subIssues []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			title := strings.TrimLeft(trimmed, "-* ")
			if title != "" {
				subIssues = append(subIssues, title)
			}
		}
	}

	var checklist strings.Builder
	checklist.WriteString(fmt.Sprintf("Sub-issues created from #%d:\n\n", parentNumber))

	for _, title := range subIssues {
		body := fmt.Sprintf("Parent issue: #%d\n\nSub-task: %s", parentNumber, title)
		created, err := gh.CreateIssue(ctx, title, body, []string{"fabriquilla:ready"})
		if err != nil {
			return fmt.Errorf("create sub-issue %q: %w", title, err)
		}
		checklist.WriteString(fmt.Sprintf("- [ ] #%d — %s\n", created.Number, title))
		slog.Info("created sub-issue", "parent", parentNumber, "child", created.Number, "title", title)
	}

	return gh.CreateComment(ctx, parentNumber, checklist.String())
}
