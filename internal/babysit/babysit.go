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

// Run polls the PR until it is merged/closed, the context is cancelled, or the
// loop budget is exhausted. A non-positive MaxIterations means "watch forever"
// — the loop only ends when the PR resolves or the process is stopped.
func Run(ctx context.Context, d Deps, o Options) error {
	if d.Forge == nil {
		return domain.InvalidInputf("babysit requires a configured forge")
	}
	if o.Interval <= 0 {
		o.Interval = 30 * time.Second
	}
	if d.Log == nil {
		l := zerolog.Nop()
		d.Log = &l
	}
	d.Log.Info().Int("pr", o.PR).Dur("interval", o.Interval).Bool("auto_fix", o.AutoFix).Msg("babysit: watching")
	backoff := o.Interval
	fixes := 0
	notifiedGreen := false
	processed := map[string]bool{} // review-comment keys already handled

	// Seed already-handled feedback: any comment at or before our most recent
	// reply is treated as done, so a (re)start never re-handles old comments and
	// posts duplicate replies. Feedback newer than the last reply — or on a PR we
	// have never replied to — is left to be addressed.
	if existing, lerr := d.Forge.ListComments(ctx, d.Repo, o.PR); lerr == nil {
		var lastReply time.Time
		for _, c := range existing {
			if isShepherdComment(c) && c.CreatedAt.After(lastReply) {
				lastReply = c.CreatedAt
			}
		}
		seeded := 0
		for _, c := range existing {
			if !c.CreatedAt.IsZero() && !c.CreatedAt.After(lastReply) {
				processed[commentKey(c)] = true
				seeded++
			}
		}
		if seeded > 0 {
			d.Log.Info().Int("pr", o.PR).Int("seen", seeded).Msg("babysit: prior feedback already addressed; watching for new feedback")
		}
	}

	for iter := 0; o.MaxIterations <= 0 || iter < o.MaxIterations; iter++ {
		pr, err := d.Forge.GetPR(ctx, d.Repo, o.PR)
		if err != nil {
			return err
		}
		if pr.State != domain.PRStateOpen {
			d.notify(ctx, "info", fmt.Sprintf("PR #%d is %s", pr.Number, pr.State), "", pr.URL, nil)
			return nil
		}

		// Review feedback: reconcile new human comments before looking at CI.
		if fresh := d.freshFeedback(ctx, pr, processed); len(fresh) > 0 {
			switch {
			case !o.AutoFix:
				d.notify(ctx, "info", fmt.Sprintf("PR #%d has new review feedback", pr.Number), feedbackTitles(fresh), pr.URL, nil)
			case fixes >= o.MaxFixAttempts:
				d.notify(ctx, "warn", fmt.Sprintf("PR #%d has new feedback but the fix budget is exhausted", pr.Number), feedbackTitles(fresh), pr.URL, nil)
			case !safeToFix(pr):
				d.notify(ctx, "warn", fmt.Sprintf("PR #%d feedback needs a human (PR is conflicted)", pr.Number), feedbackTitles(fresh), pr.URL, nil)
			default:
				d.Log.Info().Int("pr", pr.Number).Int("comments", len(fresh)).Str("feedback", feedbackTitles(fresh)).Msg("babysit: new review feedback — reconciling")
				fixes++
				if replyKey, err := d.reconcileFeedback(ctx, pr, fresh); err != nil {
					d.notify(ctx, "error", fmt.Sprintf("reconciling feedback on PR #%d failed", pr.Number), err.Error(), pr.URL, nil)
				} else {
					if replyKey != "" {
						processed[replyKey] = true // never treat our own reply as new feedback
					}
					d.Log.Info().Int("pr", pr.Number).Msg("babysit: pushed an update addressing review feedback")
					notifiedGreen = false
					backoff = o.Interval
					if !sleep(ctx, backoff) {
						return ctx.Err()
					}
					continue
				}
			}
		}

		checks, _ := d.Forge.ListChecks(ctx, d.Repo, o.PR)
		sum := domain.Summarize(checks)
		d.Log.Info().Int("pr", pr.Number).Bool("all_pass", sum.AllPass).Int("pending", sum.Pending).Bool("failing", sum.AnyFail).Msg("babysit: checked PR")

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
			backoff = capDur(backoff*3/2, 5*time.Minute) // green & idle: ease off polling

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
			d.Log.Info().Int("pr", pr.Number).Str("failing", failedNames(sum)).Msg("babysit: fixing failing checks")
			fixes++
			if err := d.attemptFix(ctx, pr, sum); err != nil {
				d.notify(ctx, "error", fmt.Sprintf("auto-fix for PR #%d failed", pr.Number), err.Error(), pr.URL, nil)
				return err
			}
			if reply, perr := d.Forge.PostComment(ctx, d.Repo, o.PR,
				"🐑 shepherd pushed an automated fix for failing checks ("+failedNames(sum)+"). Watching the new run."); perr == nil {
				processed[commentKey(reply)] = true // don't react to our own fix note
			}
			backoff = o.Interval

		default:
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
		}
	}

	if o.MaxIterations > 0 {
		d.notify(ctx, "warn", fmt.Sprintf("babysit gave up on PR #%d after %d iterations", o.PR, o.MaxIterations), "", "", nil)
	}
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
		"Pull request #%d has failing CI checks: %s.\nInvestigate and fix them in this worktree, then re-run the relevant build/test commands yourself to confirm they pass. Change only what is necessary; do not touch unrelated code.",
		pr.Number, failedNames(sum))
	if _, err := d.Agent.Headless(ctx, wt, agent.HeadlessSpec{
		Spec:         agent.Spec{Prompt: prompt, PermissionMode: agent.PermissionBypass},
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

// freshFeedback returns human review comments not yet handled, marking every
// comment it inspects as processed so each is surfaced at most once. Shepherd's
// own replies (tagged with 🐑) are skipped — necessary because gh posts them as
// the authenticated user, so they are indistinguishable by author.
func (d Deps) freshFeedback(ctx context.Context, pr domain.PullRequest, processed map[string]bool) []domain.Comment {
	comments, err := d.Forge.ListComments(ctx, d.Repo, pr.Number)
	if err != nil {
		return nil
	}
	var fresh []domain.Comment
	for _, c := range comments {
		if strings.TrimSpace(c.Body) == "" {
			continue
		}
		key := commentKey(c)
		if processed[key] {
			continue
		}
		processed[key] = true
		if isShepherdComment(c) {
			continue
		}
		fresh = append(fresh, c)
	}
	return fresh
}

// commentKey is a stable identity for a comment used to handle it at most once.
// It prefers the URL because that is stable between posting a comment (PostComment
// returns the URL) and re-fetching it later — so shepherd can mark its own replies
// handled the instant it posts them and never react to them as feedback.
func commentKey(c domain.Comment) string {
	if c.URL != "" {
		return c.URL
	}
	if c.ID != "" {
		return c.ID
	}
	return c.Author + "|" + c.CreatedAt.String() + "|" + c.Body
}

// reconcileFeedback asks the agent to assess each point of feedback, implement
// the valid ones (and self-verify), then replies on the PR with what it did. It
// returns the key of the reply it posted so the caller can mark it handled (so
// the watcher never reacts to its own summary).
func (d Deps) reconcileFeedback(ctx context.Context, pr domain.PullRequest, fresh []domain.Comment) (string, error) {
	if d.Agent == nil {
		return "", domain.InvalidInputf("reconciling feedback requires the claude CLI")
	}
	wt, err := d.Worktrees.Get(ctx, pr.HeadRef)
	if err != nil {
		return "", fmt.Errorf("no local worktree for branch %q to reconcile in (run `shepherd new` for it first): %w", pr.HeadRef, err)
	}
	res, err := d.Agent.Headless(ctx, wt, agent.HeadlessSpec{
		Spec:         agent.Spec{Prompt: feedbackPrompt(pr, fresh), PermissionMode: agent.PermissionBypass},
		OutputFormat: "json",
	})
	if err != nil {
		return "", err
	}
	reply := strings.TrimSpace(res.Text)

	if clean, _ := gitutil.IsClean(ctx, wt.Path); !clean {
		if out, err := gitutil.Exec(ctx, wt.Path, "add", "-A"); err != nil {
			return "", fmt.Errorf("git add: %v: %s", err, out)
		}
		if out, err := gitutil.Exec(ctx, wt.Path, "commit", "-m", "shepherd: address review feedback"); err != nil {
			return "", fmt.Errorf("git commit: %v: %s", err, out)
		}
		if out, err := gitutil.Exec(ctx, wt.Path, "push"); err != nil {
			return "", fmt.Errorf("git push: %v: %s", err, out)
		}
		body := "🐑 shepherd addressed the review feedback and pushed an update."
		if reply != "" {
			body += "\n\n" + clip(reply, 1500)
		}
		posted, _ := d.Forge.PostComment(ctx, d.Repo, pr.Number, body)
		return commentKey(posted), nil
	}

	body := "🐑 shepherd reviewed the feedback; no code change was needed."
	if reply != "" {
		body += "\n\n" + clip(reply, 1500)
	}
	posted, _ := d.Forge.PostComment(ctx, d.Repo, pr.Number, body)
	return commentKey(posted), nil
}

func feedbackPrompt(pr domain.PullRequest, comments []domain.Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are addressing reviewer feedback on pull request #%d (%q) from inside its worktree.\n\n", pr.Number, pr.Title)
	b.WriteString("Feedback:\n")
	for _, c := range comments {
		if c.Path != "" {
			fmt.Fprintf(&b, "- [%s:%d] %s\n", c.Path, c.Line, oneLine(c.Body))
		} else {
			fmt.Fprintf(&b, "- %s\n", oneLine(c.Body))
		}
	}
	b.WriteString("\nFor each point, decide whether it is correct and worth doing. " +
		"If it is, make the change here and re-run the relevant build/test commands to confirm everything still passes. " +
		"If you disagree with a point, leave the code unchanged for it. " +
		"End with a short summary: what you changed and why, and for anything you left unchanged, a brief, respectful explanation.")
	return b.String()
}

func isShepherdComment(c domain.Comment) bool { return strings.Contains(c.Body, "🐑") }

func feedbackTitles(cs []domain.Comment) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, oneLine(c.Body))
	}
	return strings.Join(parts, " | ")
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return clip(s, 140)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
