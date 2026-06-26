// Package babysit watches a PR's checks, auto-fixes safe failures by re-invoking
// claude in the PR's local worktree, and notifies on anything it cannot fix.
package babysit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/agent"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/forge"
	"github.com/JacobRWebb/shepherd/internal/gitutil"
	"github.com/JacobRWebb/shepherd/internal/notify"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// Deps are babysit's collaborators.
type Deps struct {
	Forge     forge.Forge
	Agent     agent.Launcher
	Worktrees worktree.Manager
	Notifier  notify.Notifier
	Repo      domain.Repo
	Log       *zerolog.Logger
}

// Options configure a babysit run.
type Options struct {
	PR             int
	Interval       time.Duration
	MaxIterations  int
	MaxFixAttempts int
	AutoFix        bool
}

// Run polls the PR until it is resolved, merged/closed, or the loop budget is
// exhausted.
func Run(ctx context.Context, d Deps, o Options) error {
	if d.Forge == nil {
		return domain.InvalidInputf("babysit requires a configured forge")
	}
	if o.Interval <= 0 {
		o.Interval = 30 * time.Second
	}
	backoff := o.Interval
	fixes := 0
	notifiedGreen := false

	for iter := 0; iter < o.MaxIterations; iter++ {
		pr, err := d.Forge.GetPR(ctx, d.Repo, o.PR)
		if err != nil {
			return err
		}
		if pr.State != domain.PRStateOpen {
			d.notify(ctx, "info", fmt.Sprintf("PR #%d is %s", pr.Number, pr.State), "", pr.URL, nil)
			return nil
		}

		checks, _ := d.Forge.ListChecks(ctx, d.Repo, o.PR)
		sum := domain.Summarize(checks)

		switch {
		case sum.AllPass:
			if !notifiedGreen {
				d.notify(ctx, "success", fmt.Sprintf("PR #%d checks are green", pr.Number), "", pr.URL, checks)
				notifiedGreen = true
			}
			if strings.EqualFold(pr.ReviewState, "APPROVED") {
				d.notify(ctx, "success", fmt.Sprintf("PR #%d is green and approved", pr.Number), "ready to merge", pr.URL, nil)
				return nil
			}
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}

		case sum.Pending > 0 && !sum.AnyFail:
			notifiedGreen = false
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = capDur(backoff*3/2, 5*time.Minute)

		case sum.AnyFail:
			notifiedGreen = false
			if !o.AutoFix || fixes >= o.MaxFixAttempts || !safeToFix(pr) {
				d.notify(ctx, "error",
					fmt.Sprintf("PR #%d has failing checks that need attention", pr.Number),
					"failing: "+failedNames(sum), pr.URL, sum.Failed)
				return nil
			}
			fixes++
			if err := d.attemptFix(ctx, pr, sum); err != nil {
				d.notify(ctx, "error", fmt.Sprintf("auto-fix for PR #%d failed", pr.Number), err.Error(), pr.URL, nil)
				return err
			}
			_, _ = d.Forge.PostComment(ctx, d.Repo, o.PR,
				"🐑 shepherd pushed an automated fix for failing checks ("+failedNames(sum)+"). Watching the new run.")
			backoff = o.Interval

		default:
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
		}
	}

	d.notify(ctx, "warn", fmt.Sprintf("babysit gave up on PR #%d after %d iterations", o.PR, o.MaxIterations), "", "", nil)
	return nil
}

// safeToFix permits auto-fixing only for an open, non-conflicting PR.
func safeToFix(pr domain.PullRequest) bool {
	if pr.State != domain.PRStateOpen {
		return false
	}
	if pr.Mergeable != nil && !*pr.Mergeable {
		return false // unresolved merge conflict — needs a human
	}
	return true
}

func (d Deps) attemptFix(ctx context.Context, pr domain.PullRequest, sum domain.CheckSummary) error {
	if d.Agent == nil {
		return domain.InvalidInputf("auto-fix requires the claude CLI")
	}
	wt, err := d.Worktrees.Get(ctx, pr.HeadRef)
	if err != nil {
		return fmt.Errorf("no local worktree for branch %q to fix in (run `shepherd new` for it first): %w", pr.HeadRef, err)
	}
	prompt := fmt.Sprintf(
		"Pull request #%d has failing CI checks: %s.\nInvestigate and fix them in this worktree. Change only what is necessary; do not touch unrelated code.",
		pr.Number, failedNames(sum))
	if _, err := d.Agent.Headless(ctx, wt, agent.HeadlessSpec{
		Spec:         agent.Spec{Prompt: prompt, PermissionMode: "acceptEdits"},
		OutputFormat: "json",
	}); err != nil {
		return err
	}
	if clean, _ := gitutil.IsClean(ctx, wt.Path); clean {
		return fmt.Errorf("the agent made no changes")
	}
	if out, err := gitutil.Exec(ctx, wt.Path, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	if out, err := gitutil.Exec(ctx, wt.Path, "commit", "-m", "shepherd: auto-fix failing checks"); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}
	if out, err := gitutil.Exec(ctx, wt.Path, "push"); err != nil {
		return fmt.Errorf("git push: %v: %s", err, out)
	}
	return nil
}

func (d Deps) notify(ctx context.Context, level, title, msg, url string, checks []domain.Check) {
	if d.Notifier == nil {
		return
	}
	_ = d.Notifier.Notify(ctx, notify.Event{Level: level, Title: title, Message: msg, PRURL: url, Checks: checks})
}

func failedNames(sum domain.CheckSummary) string {
	names := make([]string, 0, len(sum.Failed))
	for _, c := range sum.Failed {
		names = append(names, c.Name)
	}
	if len(names) == 0 {
		return "(unknown)"
	}
	return strings.Join(names, ", ")
}

// sleep waits d or until ctx is done; returns false if ctx was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func capDur(d, max time.Duration) time.Duration {
	if d > max {
		return max
	}
	return d
}
