package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/harness"
	"github.com/ruromero/la-fabriquilla/sandbox"
)

// PhaseRunner is a function that executes a single pipeline phase binary.
type PhaseRunner func(ctx context.Context, cfg *config.Config, binary, statePath string, issueNumber int, sandboxImage string) error

// Orchestrator holds all dependencies needed to process a single issue
// through the full pipeline. It is constructed by the dispatcher (or smoke
// test) and called via ProcessIssue.
type Orchestrator struct {
	GH           github.Service
	Config       *config.Config
	Store        *FileStateStore
	RunPhase     PhaseRunner
	SandboxImage string
	ConfigPath   string
	IncludeDocs  []string
}

// ProcessIssue runs the full pipeline for a single GitHub issue and returns
// the final state. It is the extracted body of the dispatcher's processIssue.
func (o *Orchestrator) ProcessIssue(ctx context.Context, issue github.Issue) (*State, error) {
	log := slog.With("issue", issue.Number)

	key := StateKey(o.GH.Owner(), o.GH.Repo(), issue.Number)

	rc := harness.LoadRepoContext(ctx, o.GH, o.IncludeDocs)

	issueTitle := sandbox.SanitizeInput(issue.Title)
	issueBody := sandbox.SanitizeInput(issue.Body)
	commentHistory := loadHumanComments(ctx, o.GH, issue.Number)

	state := &State{
		RepoOwner:      o.GH.Owner(),
		RepoName:       o.GH.Repo(),
		IssueNumber:    issue.Number,
		Phase:          "init",
		IssueTitle:     issueTitle,
		IssueBody:      issueBody,
		CommentHistory: commentHistory,
		Summaries:      rc.Summaries(),
		Conventions:    rc.Conventions(),
		IncludeDocs:    o.IncludeDocs,
		StartedAt:      time.Now(),
	}

	sess, err := harness.CloneAndStartSerena(ctx, o.GH, o.Config.Serena)
	if err != nil {
		log.Warn("failed to start Serena, continuing without", "error", err)
	}
	if sess != nil {
		defer sess.Cleanup()
		state.CloneDir = sess.CloneDir
	}

	if err := o.Store.Save(ctx, key, state); err != nil {
		return state, fmt.Errorf("save initial state: %w", err)
	}

	statePath := o.Store.StatePath(key)

	log.Info("starting gather phase")
	if err := o.RunPhase(ctx, o.Config, "gatherer", statePath, issue.Number, o.SandboxImage); err != nil {
		return o.bestState(ctx, key, state), fmt.Errorf("gather phase: %w", err)
	}
	state, err = o.Store.Load(ctx, key)
	if err != nil {
		return state, fmt.Errorf("reload state after gather: %w", err)
	}
	if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
		return state, fmt.Errorf("budget exceeded after gather: %w", err)
	}

	log.Info("starting research phase")
	if err := o.RunPhase(ctx, o.Config, "researcher", statePath, issue.Number, o.SandboxImage); err != nil {
		log.Warn("research phase failed, continuing", "error", err)
	} else {
		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return state, fmt.Errorf("reload state after research: %w", err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return state, fmt.Errorf("budget exceeded after research: %w", err)
		}
	}

	log.Info("starting plan phase")
	if err := o.RunPhase(ctx, o.Config, "planner", statePath, issue.Number, o.SandboxImage); err != nil {
		return o.bestState(ctx, key, state), fmt.Errorf("plan phase: %w", err)
	}

	state, err = o.Store.Load(ctx, key)
	if err != nil {
		return state, fmt.Errorf("reload state after plan: %w", err)
	}
	if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
		return state, fmt.Errorf("budget exceeded after plan: %w", err)
	}

	switch state.PlanOutcome {
	case "needs_info":
		log.Info("planner needs more info")
		comment := fmt.Sprintf("## Factory: Additional Information Needed\n\n%s", state.PlanContent)
		if err := o.GH.CreateComment(ctx, issue.Number, comment); err != nil {
			return state, fmt.Errorf("post needs-info comment: %w", err)
		}
		o.GH.RemoveLabel(ctx, issue.Number, "fabriquilla:in-progress")
		return state, o.GH.AddLabel(ctx, issue.Number, "fabriquilla:needs-info")

	case "decompose":
		log.Info("planner decomposing issue")
		comment := fmt.Sprintf("## Factory: Issue Decomposed\n\nThis issue is too complex for a single PR. Creating sub-issues.\n\n%s", state.PlanContent)
		if err := o.GH.CreateComment(ctx, issue.Number, comment); err != nil {
			return state, fmt.Errorf("post decompose comment: %w", err)
		}
		if err := CreateSubIssues(ctx, o.GH, issue.Number, state.PlanContent); err != nil {
			return state, fmt.Errorf("create sub-issues: %w", err)
		}
		o.GH.RemoveLabel(ctx, issue.Number, "fabriquilla:in-progress")
		return state, o.GH.AddLabel(ctx, issue.Number, "fabriquilla:tracking")

	case "plan":
		log.Info("plan produced, posting to issue")
		comment := fmt.Sprintf("## Factory: Implementation Plan\n\n%s", state.PlanContent)
		if state.ResearchContext != "" {
			comment += fmt.Sprintf("\n\n<details><summary>Research Context</summary>\n\n%s\n\n</details>", state.ResearchContext)
		}
		if err := o.GH.CreateComment(ctx, issue.Number, comment); err != nil {
			return state, fmt.Errorf("post plan comment: %w", err)
		}

		log.Info("starting design phase")
		if err := o.RunPhase(ctx, o.Config, "designer", statePath, issue.Number, o.SandboxImage); err != nil {
			return o.bestState(ctx, key, state), fmt.Errorf("design phase: %w", err)
		}
		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return state, fmt.Errorf("reload state after design: %w", err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return state, fmt.Errorf("budget exceeded after design: %w", err)
		}

		log.Info("starting code phase (includes review+iterate)")
		if err := o.RunPhase(ctx, o.Config, "coder", statePath, issue.Number, o.SandboxImage); err != nil {
			return o.bestState(ctx, key, state), fmt.Errorf("code phase: %w", err)
		}
		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return state, fmt.Errorf("reload state after code: %w", err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return state, fmt.Errorf("scope/budget exceeded after code: %w", err)
		}
		if state.CoderOutcome == "plan_infeasible" {
			log.Info("coder signaled plan infeasible, entering replan loop")
			if err := o.replanLoop(ctx, key, statePath, issue.Number); err != nil {
				return o.bestState(ctx, key, state), fmt.Errorf("replan loop: %w", err)
			}
			state, err = o.Store.Load(ctx, key)
			if err != nil {
				return state, fmt.Errorf("reload state after replan: %w", err)
			}
			if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
				return state, fmt.Errorf("budget exceeded after replan: %w", err)
			}
		}
		if err := CheckPRScope(state.Files, o.Config.MaxFilesChanged, o.Config.MaxPRSizeLines); err != nil {
			return state, fmt.Errorf("scope check: %w", err)
		}
		if err := ValidateFiles(state.Files, o.Config.BlockedPaths); err != nil {
			return state, fmt.Errorf("path validation: %w", err)
		}
		if violations := ValidateContents(state.Files); len(violations) > 0 {
			return state, fmt.Errorf("secret detected in generated code: %s in %s line %d", violations[0].Pattern, violations[0].File, violations[0].Line)
		}

		repoCfg, _ := o.Config.FindRepoConfig(state.RepoOwner, state.RepoName)
		if len(repoCfg.ValidateCommands) > 0 {
			maxAttempts := o.Config.MaxIterations
			if maxAttempts < 1 {
				maxAttempts = 1
			}
			for attempt := 0; attempt < maxAttempts; attempt++ {
				log.Info("starting validate phase", "attempt", attempt+1)
				if err := o.RunPhase(ctx, o.Config, "validator", statePath, issue.Number, o.SandboxImage); err != nil {
					return o.bestState(ctx, key, state), fmt.Errorf("validate phase: %w", err)
				}
				state, err = o.Store.Load(ctx, key)
				if err != nil {
					return state, fmt.Errorf("reload state after validate: %w", err)
				}
				if state.ValidatePass {
					log.Info("validation passed")
					break
				}
				if attempt == maxAttempts-1 {
					return state, fmt.Errorf("validation failed after %d attempts", attempt+1)
				}
				log.Info("validation failed, retrying coder with feedback")
				state.ReplanFeedback = fmt.Sprintf("Build/test validation failed. Fix the errors and regenerate all files.\n\n%s", state.ValidateOutput)
				if err := o.Store.Save(ctx, key, state); err != nil {
					return state, fmt.Errorf("save validation feedback: %w", err)
				}
				if err := o.RunPhase(ctx, o.Config, "coder", statePath, issue.Number, o.SandboxImage); err != nil {
					return o.bestState(ctx, key, state), fmt.Errorf("code retry after validation: %w", err)
				}
				state, err = o.Store.Load(ctx, key)
				if err != nil {
					return state, fmt.Errorf("reload state after code retry: %w", err)
				}
				if err := CheckPRScope(state.Files, o.Config.MaxFilesChanged, o.Config.MaxPRSizeLines); err != nil {
					return state, fmt.Errorf("scope check after retry: %w", err)
				}
				if err := ValidateFiles(state.Files, o.Config.BlockedPaths); err != nil {
					return state, fmt.Errorf("path validation after retry: %w", err)
				}
				if violations := ValidateContents(state.Files); len(violations) > 0 {
					return state, fmt.Errorf("secret detected after retry: %s in %s line %d", violations[0].Pattern, violations[0].File, violations[0].Line)
				}
			}
		}

		log.Info("starting commit phase")
		if err := o.RunPhase(ctx, o.Config, "committer", statePath, issue.Number, o.SandboxImage); err != nil {
			return o.bestState(ctx, key, state), fmt.Errorf("commit phase: %w", err)
		}

		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return state, fmt.Errorf("reload state after commit: %w", err)
		}

		if state.PRNumber > 0 {
			log.Info("PR created, starting review loop", "pr", state.PRNumber)
			if err := o.reviewIterateLoop(ctx, key, statePath, issue.Number); err != nil {
				log.Warn("review-iterate loop failed", "error", err)
				comment := fmt.Sprintf("## Factory: Review Loop Failed\n\nThe automated review-iterate loop failed after creating PR #%d.\n\n```\n%s\n```\n\nPlease review the PR manually.", state.PRNumber, err)
				o.GH.CreateComment(ctx, issue.Number, comment)
			}
		}
		return state, nil
	}

	return state, fmt.Errorf("unknown plan outcome: %s", state.PlanOutcome)
}

// bestState attempts to reload state from the store after a phase failure,
// returning the freshest version that may contain PR metadata written by
// the failed phase. Falls back to the provided state if reload fails.
func (o *Orchestrator) bestState(ctx context.Context, key string, fallback *State) *State {
	if loaded, err := o.Store.Load(ctx, key); err == nil {
		return loaded
	}
	return fallback
}

// reviewIterateLoop runs the reviewer/iterator loop up to Config.MaxIterations times.
func (o *Orchestrator) reviewIterateLoop(ctx context.Context, key, statePath string, issueNumber int) error {
	for i := 0; i < o.Config.MaxIterations; i++ {
		slog.Info("starting review iteration", "iteration", i+1, "max", o.Config.MaxIterations)

		if err := o.RunPhase(ctx, o.Config, "reviewer", statePath, issueNumber, o.SandboxImage); err != nil {
			return fmt.Errorf("reviewer (iteration %d): %w", i+1, err)
		}

		state, err := o.Store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after review (iteration %d): %w", i+1, err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after review (iteration %d): %w", i+1, err)
		}

		needsIteration := false
		if o.Config.Arbiter.Model != "" && state.ArbiterResult != nil {
			needsIteration = ArbiterNeedsIteration(state.ArbiterResult.Findings)
		} else if state.Review != nil {
			needsIteration = ReviewNeedsIteration(state.Review.Findings)
		}

		if !needsIteration {
			slog.Info("review clean", "iterations", i+1)
			return nil
		}

		slog.Info("review found issues, running iterator", "iteration", i+1)
		if err := o.RunPhase(ctx, o.Config, "iterator", statePath, issueNumber, o.SandboxImage); err != nil {
			return fmt.Errorf("iterator (iteration %d): %w", i+1, err)
		}

		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after iterate (iteration %d): %w", i+1, err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after iterate (iteration %d): %w", i+1, err)
		}
	}

	slog.Warn("max review iterations reached", "max", o.Config.MaxIterations)
	return nil
}

// replanLoop re-runs planner → designer → coder when the coder signals
// plan infeasibility, up to Config.MaxReplans times.
func (o *Orchestrator) replanLoop(ctx context.Context, key, statePath string, issueNumber int) error {
	for i := 0; i < o.Config.MaxReplans; i++ {
		slog.Info("starting replan cycle", "attempt", i+1, "max", o.Config.MaxReplans)

		state, err := o.Store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state for replan: %w", err)
		}

		reason := sandbox.SanitizeInput(state.InfeasibleReason)

		state.PlanOutcome = ""
		state.PlanContent = ""
		state.Design = ""
		state.Code = ""
		state.Review = nil
		state.ArbiterResult = nil
		state.Files = nil
		state.CoderOutcome = ""
		state.InfeasibleReason = ""
		state.ReplanFeedback = reason
		state.ReplanCount++
		if err := o.Store.Save(ctx, key, state); err != nil {
			return fmt.Errorf("save cleaned state for replan: %w", err)
		}

		if err := o.RunPhase(ctx, o.Config, "planner", statePath, issueNumber, o.SandboxImage); err != nil {
			return fmt.Errorf("replan planner (attempt %d): %w", i+1, err)
		}
		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after replan planner: %w", err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after replan planner: %w", err)
		}

		if state.PlanOutcome != "plan" {
			if err := o.GH.AddLabel(ctx, issueNumber, "fabriquilla:needs-human"); err != nil {
				slog.Warn("failed to add needs-human label during replan", "issue", issueNumber, "error", err)
			}
			comment := fmt.Sprintf("## Factory: Plan Infeasible\n\nThe coder determined the plan cannot be implemented as designed. "+
				"Re-planning was attempted but the planner returned %q instead of a revised plan.\n\n"+
				"**Infeasibility reason:**\n%s\n\nThis issue needs human attention.", state.PlanOutcome, reason)
			if err := o.GH.CreateComment(ctx, issueNumber, comment); err != nil {
				slog.Warn("failed to post replan escalation comment", "issue", issueNumber, "error", err)
			}
			return fmt.Errorf("replanner returned %q instead of plan", state.PlanOutcome)
		}

		if err := o.RunPhase(ctx, o.Config, "designer", statePath, issueNumber, o.SandboxImage); err != nil {
			return fmt.Errorf("replan designer (attempt %d): %w", i+1, err)
		}
		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after replan designer: %w", err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after replan designer: %w", err)
		}

		if err := o.RunPhase(ctx, o.Config, "coder", statePath, issueNumber, o.SandboxImage); err != nil {
			return fmt.Errorf("replan coder (attempt %d): %w", i+1, err)
		}
		state, err = o.Store.Load(ctx, key)
		if err != nil {
			return fmt.Errorf("reload state after replan coder: %w", err)
		}
		if err := CheckCostBudget(state, o.Config.MaxCostBudget); err != nil {
			return fmt.Errorf("budget exceeded after replan coder: %w", err)
		}

		if state.CoderOutcome != "plan_infeasible" {
			slog.Info("replan succeeded", "attempts", i+1)
			return nil
		}
	}

	state, err := o.Store.Load(ctx, key)
	if err != nil {
		return fmt.Errorf("reload state after replan exhausted: %w", err)
	}

	if err := o.GH.AddLabel(ctx, issueNumber, "fabriquilla:needs-human"); err != nil {
		slog.Warn("failed to add needs-human label after replan exhausted", "issue", issueNumber, "error", err)
	}
	sanitizedReason := sandbox.SanitizeInput(state.InfeasibleReason)
	comment := fmt.Sprintf("## Factory: Plan Infeasible\n\nThe coder determined the plan cannot be implemented as designed. "+
		"Re-planning was attempted but did not produce a viable plan after %d attempt(s).\n\n"+
		"**Last infeasibility reason:**\n%s\n\nThis issue needs human attention.", state.ReplanCount, sanitizedReason)
	if err := o.GH.CreateComment(ctx, issueNumber, comment); err != nil {
		slog.Warn("failed to post replan exhaustion comment", "issue", issueNumber, "error", err)
	}
	return fmt.Errorf("plan infeasible after %d replan attempts", state.ReplanCount)
}

// loadHumanComments fetches non-bot comments for an issue and returns them
// formatted as a markdown string.
func loadHumanComments(ctx context.Context, gh github.Service, issueNumber int) string {
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

// CreateSubIssues parses bullet-list items from decomposeContent and creates a
// child GitHub issue for each one, then posts a checklist comment on the parent.
func CreateSubIssues(ctx context.Context, gh github.Service, parentNumber int, decomposeContent string) error {
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
