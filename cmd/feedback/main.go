package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ruromero/la-fabriquilla/agents"
	helpers "github.com/ruromero/la-fabriquilla/cmd/internal"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/pipeline"
	"github.com/ruromero/la-fabriquilla/review"
	"github.com/ruromero/la-fabriquilla/traces"
)

// FeedbackEntry is a structured log record written to the JSONL feedback log
// after each external review cycle.
type FeedbackEntry struct {
	Timestamp          time.Time      `json:"timestamp"`
	RepoOwner          string         `json:"repo_owner"`
	RepoName           string         `json:"repo_name"`
	IssueNumber        int            `json:"issue_number"`
	PRNumber           int            `json:"pr_number"`
	Iteration          int            `json:"iteration"`
	ExternalSource     string         `json:"external_source"`
	FindingsTotal      int            `json:"findings_total"`
	FindingsByCategory map[string]int `json:"findings_by_category"`
	FixedByIterator    int            `json:"fixed_by_iterator"`
	Dismissed          int            `json:"dismissed"`
	RootCausesCreated  int            `json:"root_causes_created"`
}

const maxRootCauseIssues = 3

func main() {
	cfg, state := helpers.MustLoadConfigAndState()

	if state.PRNumber <= 0 {
		slog.Error("no PR number in state — feedback requires an existing PR")
		os.Exit(1)
	}

	gh := helpers.MustGitHubClientForApp(cfg, "committer", state)
	rc := &github.ReviewClient{Client: gh}
	adapter := &review.QodoAdapter{}
	ctx := context.Background()

	timeout := cfg.PhaseDuration("feedback")
	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = 3
	}

	stateDir := cfg.StateDir
	if stateDir == "" {
		stateDir = "/data/pipeline"
	}

	for iter := 0; iter < maxIter; iter++ {
		slog.Info("feedback iteration starting", "iteration", iter+1, "max", maxIter)

		// Step a: trigger external review
		if err := adapter.TriggerReview(ctx, rc, state.PRNumber); err != nil {
			slog.Error("failed to trigger external review", "error", err)
			os.Exit(1)
		}

		// Step b: poll until review ready or timeout
		deadline := time.Now().Add(timeout)
		ready := false
		for time.Now().Before(deadline) {
			var err error
			ready, err = adapter.ReviewReady(ctx, rc, state.PRNumber)
			if err != nil {
				slog.Error("failed to check review readiness", "error", err)
				os.Exit(1)
			}
			if ready {
				break
			}
			time.Sleep(30 * time.Second)
		}
		if !ready {
			slog.Error("external review timed out", "timeout", timeout)
			os.Exit(1)
		}

		// Step c: parse findings
		findings, err := adapter.ParseFindings(ctx, rc, state.PRNumber)
		if err != nil {
			slog.Error("failed to parse external findings", "error", err)
			os.Exit(1)
		}

		// Step d: no findings means clean review
		if len(findings) == 0 {
			slog.Info("external review clean — no findings")
			logFeedback(stateDir, FeedbackEntry{
				Timestamp:          time.Now(),
				RepoOwner:          state.RepoOwner,
				RepoName:           state.RepoName,
				IssueNumber:        state.IssueNumber,
				PRNumber:           state.PRNumber,
				Iteration:          iter + 1,
				ExternalSource:     "qodo",
				FindingsTotal:      0,
				FindingsByCategory: map[string]int{},
			})
			break
		}

		slog.Info("external findings received", "count", len(findings))

		// Step e: arbiter classification (if configured)
		var arbiterFindings []review.ArbiterFinding
		if cfg.Arbiter.Model != "" {
			arbModel, arbURL, arbKey, arbErr := cfg.ResolveModel(cfg.Arbiter.Model)
			if arbErr != nil {
				slog.Error("resolve arbiter model", "error", arbErr)
				os.Exit(1)
			}
			arbCl := inference.NewClient(arbURL, inference.WithAPIKey(arbKey))

			var dismissedKeys []string
			if state.ArbiterResult != nil {
				dismissedKeys = state.ArbiterResult.DismissedKeys
			}

			arbStart := time.Now()
			arb, arbErr2 := agents.Arbitrate(ctx, arbCl, arbModel,
				findings, state.Conventions, state.Summaries, state.PlanContent,
				dismissedKeys)
			arbElapsed := time.Since(arbStart)
			if arbErr2 != nil {
				slog.Error("arbiter phase failed", "error", arbErr2)
				os.Exit(1)
			}

			state.RecordTokenUsage("feedback-arbiter", arb.Model, arb.PromptTokens, arb.CompTokens, 0, arbElapsed.Seconds())
			traces.Log(traces.Trace{
				IssueNumber:     state.IssueNumber,
				Phase:           "feedback-arbiter",
				Model:           arb.Model,
				PromptTokens:    arb.PromptTokens,
				CompTokens:      arb.CompTokens,
				Duration:        arbElapsed.String(),
				StartedAt:       arbStart,
				CumPromptTokens: state.TotalPromptTokens,
				CumCompTokens:   state.TotalCompTokens,
				CumCostUSD:      state.TotalCostUSD,
			})

			arbiterFindings = arb.Result.Findings
		} else {
			// No arbiter: treat all findings as fix_here
			for _, f := range findings {
				arbiterFindings = append(arbiterFindings, review.ArbiterFinding{
					Finding:        f,
					Classification: review.ClassFixHere,
					Reason:         "no arbiter configured",
				})
			}
		}

		// Classify findings by category
		categoryCounts := classifyByCategory(arbiterFindings)
		dismissed := countClassification(arbiterFindings, review.ClassDismissed)
		fixHere := countClassification(arbiterFindings, review.ClassFixHere)
		subtask := countClassification(arbiterFindings, review.ClassSubtask)
		rootCause := countClassification(arbiterFindings, review.ClassRootCause)

		slog.Info("feedback findings classified",
			"fix_here", fixHere,
			"subtask", subtask,
			"root_cause", rootCause,
			"dismissed", dismissed,
		)

		// Step f: create root cause issues (max 3)
		rootCausesCreated := 0
		for _, af := range arbiterFindings {
			if af.Classification != review.ClassRootCause {
				continue
			}
			if rootCausesCreated >= maxRootCauseIssues {
				slog.Warn("root cause issue limit reached", "max", maxRootCauseIssues)
				break
			}
			title := af.ProposedTitle
			if title == "" {
				title = af.Finding.Title
			}
			body := fmt.Sprintf("Root cause identified in PR #%d\n\nFinding: %s\n\n%s",
				state.PRNumber, af.Finding.Title, af.Finding.Detail)
			if _, err := gh.CreateIssue(ctx, title, body, []string{"fabriquilla:ready"}); err != nil {
				slog.Error("failed to create root cause issue", "error", err, "title", title)
			} else {
				rootCausesCreated++
				slog.Info("root cause issue created", "title", title)
			}
		}

		// Step h: log structured feedback entry
		logFeedback(stateDir, FeedbackEntry{
			Timestamp:          time.Now(),
			RepoOwner:          state.RepoOwner,
			RepoName:           state.RepoName,
			IssueNumber:        state.IssueNumber,
			PRNumber:           state.PRNumber,
			Iteration:          iter + 1,
			ExternalSource:     "qodo",
			FindingsTotal:      len(arbiterFindings),
			FindingsByCategory: categoryCounts,
			FixedByIterator:    0,
			Dismissed:          dismissed,
			RootCausesCreated:  rootCausesCreated,
		})

		// Step g/i: if fix_here or subtask findings exist, update state for dispatcher
		if fixHere+subtask > 0 {
			state.Review = &pipeline.ReviewState{
				Findings: findings,
			}

			var newDismissed []string
			if state.ArbiterResult != nil {
				newDismissed = append(newDismissed, state.ArbiterResult.DismissedKeys...)
			}
			for _, af := range arbiterFindings {
				if af.Classification == review.ClassDismissed {
					newDismissed = append(newDismissed, review.DismissKey(af.Finding))
				}
			}
			state.ArbiterResult = &pipeline.ArbiterState{
				Findings:      arbiterFindings,
				DismissedKeys: newDismissed,
			}
			state.Phase = "feedback-iterate"
			slog.Info("fix_here/subtask findings remain — handing back to dispatcher",
				"fix_here", fixHere, "subtask", subtask)
			break
		}

		// Only root_cause and/or dismissed — loop for next iteration
		slog.Info("no actionable findings in this iteration, continuing")
	}

	helpers.MustSaveState(state)
}

func countClassification(findings []review.ArbiterFinding, c review.Classification) int {
	n := 0
	for _, f := range findings {
		if f.Classification == c {
			n++
		}
	}
	return n
}

func classifyByCategory(findings []review.ArbiterFinding) map[string]int {
	counts := make(map[string]int)
	for _, f := range findings {
		counts[string(f.Finding.Category)]++
	}
	return counts
}

func logFeedback(stateDir string, entry FeedbackEntry) {
	dir := filepath.Join(stateDir, "feedback", entry.RepoOwner, entry.RepoName)
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Error("failed to create feedback log dir", "error", err)
		return
	}
	logPath := filepath.Join(dir, "log.jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		slog.Error("failed to open feedback log", "error", err)
		return
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		slog.Error("failed to marshal feedback entry", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		slog.Error("failed to write feedback entry", "error", err)
	}
}
