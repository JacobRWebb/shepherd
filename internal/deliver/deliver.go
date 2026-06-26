// Package deliver runs the full autonomous loop for a single idea: an agent
// studies the repo and proposes an approach (grounded on the base branch),
// Shepherd opens a worktree, an agent implements and self-verifies, the
// validation gate runs (with bounded auto-fix), a PR is opened, and — unless
// disabled — babysit watches CI and reconciles review feedback until the human
// merges. The two human touchpoints are the idea at the start and the merge
// (or review feedback) at the end.
package deliver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/agent"
	"github.com/JacobRWebb/shepherd/internal/babysit"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/forge"
	"github.com/JacobRWebb/shepherd/internal/notify"
	"github.com/JacobRWebb/shepherd/internal/pipeline"
	"github.com/JacobRWebb/shepherd/internal/ship"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// Deps are deliver's collaborators.
type Deps struct {
	Worktrees worktree.Manager
	Agent     agent.Launcher
	Forge     forge.Forge
	Runner    *pipeline.Runner
	Notifier  notify.Notifier
	Repo      domain.Repo
	RepoRoot  string
	Log       *zerolog.Logger
}

// Options configure a deliver run.
type Options struct {
	Idea      string
	Base      string
	Model     string
	Design    bool // run the design pass that produces a plan (default true)
	Discuss   bool // hand the terminal to an interactive planning session first
	Draft     bool
	Reviewers []string

	MaxFixAttempts       int
	Babysit              bool
	BabysitInterval      time.Duration
	BabysitMaxIterations int
}

// Result is the structured outcome of a deliver run.
type Result struct {
	Idea    string      `json:"idea"`
	Plan    string      `json:"plan,omitempty"`
	Branch  string      `json:"branch"`
	Dir     string      `json:"dir"`
	Ship    ship.Result `json:"ship"`
	PRURL   string      `json:"pr_url,omitempty"`
	Babysat bool        `json:"babysat"`
}

// Run executes the full loop for one idea.
func Run(ctx context.Context, d Deps, o Options) (Result, error) {
	if d.Agent == nil {
		return Result{}, domain.InvalidInputf("deliver requires the claude CLI")
	}
	res := Result{Idea: o.Idea}

	// 1. Design — discuss the feature and the best implementation, grounded on
	//    the current code. Optional interactive handoff first, then a plan pass.
	if o.Discuss {
		d.Log.Info().Msg("deliver: opening an interactive planning session (plan mode)")
		if _, err := d.Agent.Interactive(ctx, domain.Worktree{Path: d.RepoRoot}, agent.InteractiveSpec{
			Spec: agent.Spec{Prompt: discussPrompt(o), PermissionMode: "plan"},
		}); err != nil {
			d.Log.Warn().Err(err).Msg("deliver: interactive planning failed; continuing")
		}
	}
	if o.Design {
		d.Log.Info().Msg("deliver: designing the implementation")
		if plan, err := design(ctx, d, o); err != nil {
			d.Log.Warn().Err(err).Msg("deliver: design pass failed; implementing from the idea alone")
		} else {
			res.Plan = plan
		}
	}

	// 2. Worktree off the base branch.
	task := domain.NewTask(o.Idea)
	wt, err := d.Worktrees.Create(ctx, task, o.Base)
	if err != nil {
		return res, fmt.Errorf("creating worktree: %w", err)
	}
	res.Branch = wt.Branch
	res.Dir = wt.Path
	d.Log.Info().Str("branch", wt.Branch).Str("dir", wt.Path).Msg("deliver: worktree ready")

	// 3. Implement (the agent edits and self-verifies).
	d.Log.Info().Msg("deliver: implementing")
	if err := implement(ctx, d, o, wt, res.Plan); err != nil {
		return res, fmt.Errorf("implementing: %w", err)
	}

	// 4 + 5. Validation gate (with bounded auto-fix) then push + open PR.
	d.Log.Info().Msg("deliver: validating and shipping")
	sres, err := ship.Run(ctx, ship.Deps{
		Worktrees: d.Worktrees,
		Forge:     d.Forge,
		Agent:     d.Agent,
		Runner:    d.Runner,
		Repo:      d.Repo,
		RepoRoot:  d.RepoRoot,
		Log:       d.Log,
	}, ship.Options{
		Target:         wt.Branch,
		Base:           o.Base,
		Title:          deliverTitle(o.Idea),
		Body:           prBody(o.Idea, res.Plan),
		Draft:          o.Draft,
		AutoFix:        true,
		MaxFixAttempts: o.MaxFixAttempts,
		Reviewers:      o.Reviewers,
	})
	res.Ship = sres
	if err != nil {
		return res, err
	}
	if sres.PR != nil {
		res.PRURL = sres.PR.URL
	}

	// 6. Babysit the PR loop until the human merges.
	if o.Babysit && sres.PR != nil {
		res.Babysat = true
		d.Log.Info().Int("pr", sres.PR.Number).Msg("deliver: babysitting the PR until merge (Ctrl-C stops watching; the PR stays open)")
		if err := babysit.Run(ctx, babysit.Deps{
			Forge:     d.Forge,
			Agent:     d.Agent,
			Worktrees: d.Worktrees,
			Notifier:  d.Notifier,
			Repo:      d.Repo,
			Log:       d.Log,
		}, babysit.Options{
			PR:             sres.PR.Number,
			Interval:       o.BabysitInterval,
			MaxIterations:  o.BabysitMaxIterations,
			MaxFixAttempts: o.MaxFixAttempts,
			AutoFix:        true,
		}); err != nil {
			return res, err
		}
	}
	return res, nil
}

func design(ctx context.Context, d Deps, o Options) (string, error) {
	prompt := fmt.Sprintf(
		"We are about to implement an idea in THIS repository, branching from %s.\n\n"+
			"Idea: %s\n\n"+
			"Study the existing code and propose the best, smallest implementation that fits the project's "+
			"conventions. Output a concise plan: which files to add/change, the approach, and how to validate it. "+
			"Plan only — do not write any code yet.",
		baseLabel(o.Base), o.Idea)
	res, err := d.Agent.Headless(ctx, domain.Worktree{Path: d.RepoRoot}, agent.HeadlessSpec{
		Spec:         agent.Spec{Prompt: prompt, Model: o.Model, PermissionMode: "plan"},
		OutputFormat: "json",
	})
	if err != nil {
		return "", err
	}
	plan := strings.TrimSpace(res.Text)
	if plan == "" {
		return "", fmt.Errorf("design pass produced no plan")
	}
	return plan, nil
}

func implement(ctx context.Context, d Deps, o Options, wt domain.Worktree, plan string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Implement the following idea end to end in this worktree.\n\nIdea: %s\n\n", o.Idea)
	if plan != "" {
		fmt.Fprintf(&b, "Agreed implementation plan:\n%s\n\n", plan)
	}
	b.WriteString("Write the code and any tests it needs. Then run the project's build and test commands " +
		"yourself and make sure they pass before you finish. Keep the change focused and idiomatic; do not " +
		"touch unrelated code. When done, summarize what you changed.")

	_, err := d.Agent.Headless(ctx, wt, agent.HeadlessSpec{
		Spec: agent.Spec{
			Prompt:         b.String(),
			Model:          o.Model,
			PermissionMode: agent.PermissionBypass,
			SessionID:      uuid.NewString(),
		},
		OutputFormat: "json",
	})
	return err
}

func discussPrompt(o Options) string {
	return fmt.Sprintf(
		"Let's plan how to implement this idea in this repository before any code is written.\n\nIdea: %s\n\n"+
			"Discuss the approach with me and help settle on the best, smallest implementation. Plan mode — no edits.",
		o.Idea)
}

func prBody(idea, plan string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", idea)
	if strings.TrimSpace(plan) != "" {
		b.WriteString("\n## Plan\n\n")
		b.WriteString(plan)
		b.WriteString("\n")
	}
	b.WriteString("\n— delivered by shepherd")
	return b.String()
}

func deliverTitle(idea string) string {
	t := strings.TrimSpace(idea)
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if len(t) > 72 {
		t = strings.TrimSpace(t[:72]) + "…"
	}
	return t
}

func baseLabel(base string) string {
	if strings.TrimSpace(base) == "" {
		return "the default branch"
	}
	return base
}
