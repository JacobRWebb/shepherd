// Package ship implements the validate -> push -> open-PR flow with an optional
// bounded auto-fix loop.
package ship

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/agent"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/forge"
	"github.com/JacobRWebb/shepherd/internal/gitutil"
	"github.com/JacobRWebb/shepherd/internal/pipeline"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// Deps are the collaborators ship needs. Forge and Agent may be nil (PR/auto-fix
// are then skipped/errored accordingly).
type Deps struct {
	Worktrees worktree.Manager
	Forge     forge.Forge
	Agent     agent.Launcher
	Runner    *pipeline.Runner
	Repo      domain.Repo
	RepoRoot  string
	Log       *zerolog.Logger
}

// Options configure a ship run.
type Options struct {
	Target         string
	Base           string
	Title          string
	Body           string
	Draft          bool
	AutoFix        bool
	MaxFixAttempts int
	Reviewers      []string
	NoPush         bool
}

// Result is the structured outcome of ship.
type Result struct {
	Branch      string              `json:"branch"`
	Dir         string              `json:"dir"`
	GatePassed  bool                `json:"gate_passed"`
	Pipeline    pipeline.Result     `json:"pipeline"`
	FixAttempts int                 `json:"fix_attempts"`
	Pushed      bool                `json:"pushed"`
	PR          *domain.PullRequest `json:"pr,omitempty"`
}

// Run executes the gate (with optional auto-fix), then pushes and opens a PR.
func Run(ctx context.Context, d Deps, o Options) (Result, error) {
	dir, branch, err := resolveTarget(ctx, d, o.Target)
	if err != nil {
		return Result{}, err
	}
	res := Result{Branch: branch, Dir: dir}

	for {
		allowed, pres, perr := d.Runner.Gate(ctx, dir)
		if perr != nil {
			return res, perr
		}
		res.Pipeline = pres
		res.GatePassed = allowed
		if allowed {
			break
		}
		if !o.AutoFix || res.FixAttempts >= o.MaxFixAttempts {
			return res, domain.InvalidInputf("validation gate failed:\n%s", pres.FailureDigest())
		}
		res.FixAttempts++
		if err := autoFix(ctx, d, dir, branch, pres); err != nil {
			return res, err
		}
	}

	if o.NoPush {
		return res, nil
	}

	if err := commitAndPush(ctx, dir, branch, o); err != nil {
		return res, err
	}
	res.Pushed = true

	if d.Forge == nil {
		return res, nil
	}
	if existing, _ := d.Forge.PRForBranch(ctx, d.Repo, branch); existing != nil && existing.State == domain.PRStateOpen {
		res.PR = existing
		return res, nil
	}
	pr, err := d.Forge.OpenPR(ctx, d.Repo, domain.OpenPROpts{
		Title:     orStr(o.Title, defaultTitle(branch)),
		Body:      orStr(o.Body, "Opened by shepherd."),
		Head:      branch,
		Base:      o.Base,
		Draft:     o.Draft,
		Reviewers: o.Reviewers,
	})
	if err != nil {
		return res, err
	}
	res.PR = &pr
	return res, nil
}

func resolveTarget(ctx context.Context, d Deps, target string) (dir, branch string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		br, cerr := gitutil.CurrentBranch(ctx, d.RepoRoot)
		if cerr != nil {
			return "", "", cerr
		}
		if wt, e := d.Worktrees.Get(ctx, br); e == nil && !wt.IsMain {
			return wt.Path, wt.Branch, nil
		}
		return d.RepoRoot, br, nil
	}
	if wt, e := d.Worktrees.Get(ctx, target); e == nil {
		return wt.Path, wt.Branch, nil
	}
	if br, e := gitutil.CurrentBranch(ctx, d.RepoRoot); e == nil && br == target {
		return d.RepoRoot, br, nil
	}
	return "", "", domain.NotFoundf("no worktree or current branch matching %q (run `shepherd new` first)", target)
}

func commitAndPush(ctx context.Context, dir, branch string, o Options) error {
	if clean, _ := gitutil.IsClean(ctx, dir); !clean {
		if out, err := gitutil.Exec(ctx, dir, "add", "-A"); err != nil {
			return fmt.Errorf("git add: %v: %s", err, out)
		}
		msg := orStr(o.Title, "shepherd: ship "+branch)
		if out, err := gitutil.Exec(ctx, dir, "commit", "-m", msg); err != nil {
			return fmt.Errorf("git commit: %v: %s", err, out)
		}
	}
	if out, err := gitutil.Exec(ctx, dir, "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push: %v: %s", err, out)
	}
	return nil
}

func autoFix(ctx context.Context, d Deps, dir, branch string, pres pipeline.Result) error {
	if d.Agent == nil {
		return domain.InvalidInputf("--auto-fix requires the claude CLI")
	}
	prompt := "The validation pipeline failed. Fix the failing steps below. Change only what is necessary; do not touch unrelated code.\n\n" + pres.FailureDigest()
	_, err := d.Agent.Headless(ctx, domain.Worktree{Path: dir, Branch: branch}, agent.HeadlessSpec{
		Spec:         agent.Spec{Prompt: prompt, PermissionMode: "acceptEdits"},
		OutputFormat: "json",
	})
	return err
}

func defaultTitle(branch string) string {
	t := branch
	if i := strings.LastIndex(t, "/"); i >= 0 {
		t = t[i+1:]
	}
	t = strings.ReplaceAll(t, "-", " ")
	t = strings.TrimSpace(t)
	if t == "" {
		return "shepherd PR"
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

func orStr(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
