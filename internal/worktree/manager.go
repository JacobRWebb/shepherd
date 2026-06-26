// Package worktree manages isolated git worktrees by shelling out to
// `git worktree`. The Manager interface is consumed by the cli/app layers; Git
// is the only implementation.
//
// Safety invariant: Shepherd never performs a mutating operation against the
// main working tree. RunInWorktree and Remove refuse it, and Remove additionally
// refuses any path outside the configured worktrees root.
package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// Manager creates and manages git worktrees.
type Manager interface {
	Create(ctx context.Context, task domain.Task, base string) (domain.Worktree, error)
	List(ctx context.Context) ([]domain.Worktree, error)
	Get(ctx context.Context, ref string) (domain.Worktree, error)
	RunInWorktree(ctx context.Context, wt domain.Worktree, name string, args ...string) (ExecResult, error)
	Remove(ctx context.Context, wt domain.Worktree, force bool) error
	Prune(ctx context.Context, removeBranches bool) ([]domain.Worktree, error)
}

// Options configures a Git manager. The app layer maps config into these so the
// worktree package stays independent of the config schema.
type Options struct {
	RepoRoot     string // absolute main work tree
	Root         string // absolute worktrees root
	NameTemplate string
	BranchPrefix string
	DefaultBase  string // base ref when none is given (empty => HEAD)
}

// Git implements Manager over the git CLI.
type Git struct {
	opts Options
	log  *zerolog.Logger
}

var _ Manager = (*Git)(nil)

// NewGit builds a Git manager. A nil logger is replaced with a no-op logger.
func NewGit(opts Options, log *zerolog.Logger) *Git {
	if log == nil {
		l := zerolog.Nop()
		log = &l
	}
	return &Git{opts: opts, log: log}
}

func (g *Git) git(ctx context.Context, args ...string) (ExecResult, error) {
	return run(ctx, g.opts.RepoRoot, "git", args...)
}

// Create makes a new worktree+branch for task, branched from base (or the
// configured default / HEAD). On a name/branch collision it appends a short
// unique suffix.
func (g *Git) Create(ctx context.Context, task domain.Task, base string) (domain.Worktree, error) {
	if strings.TrimSpace(base) == "" {
		base = g.opts.DefaultBase
	}
	if strings.TrimSpace(base) == "" {
		base = "HEAD"
	}

	dir, branch := names(task, g.opts.NameTemplate, g.opts.BranchPrefix)
	full := filepath.Join(g.opts.Root, dir)
	if g.exists(ctx, full, branch) {
		dir, branch = withSuffix(dir, g.opts.BranchPrefix, shortID())
		full = filepath.Join(g.opts.Root, dir)
		if g.exists(ctx, full, branch) {
			return domain.Worktree{}, domain.Conflictf("worktree %q already exists", dir)
		}
	}

	if err := os.MkdirAll(g.opts.Root, 0o755); err != nil {
		return domain.Worktree{}, fmt.Errorf("creating worktrees root: %w", err)
	}

	res, err := g.git(ctx, "worktree", "add", "-b", branch, full, base)
	if err != nil {
		return domain.Worktree{}, fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(res.Stderr))
	}
	g.log.Info().Str("branch", branch).Str("path", full).Str("base", base).Msg("created worktree")
	return g.Get(ctx, branch)
}

func (g *Git) List(ctx context.Context) ([]domain.Worktree, error) {
	res, err := g.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w: %s", err, strings.TrimSpace(res.Stderr))
	}
	return parsePorcelain(res.Stdout), nil
}

func (g *Git) Get(ctx context.Context, ref string) (domain.Worktree, error) {
	wts, err := g.List(ctx)
	if err != nil {
		return domain.Worktree{}, err
	}
	ref = strings.TrimSpace(ref)
	for _, wt := range wts {
		if wt.Branch == ref || wt.Name == ref || wt.Path == ref || filepath.Base(wt.Path) == ref {
			return wt, nil
		}
	}
	return domain.Worktree{}, domain.NotFoundf("worktree %q", ref)
}

func (g *Git) RunInWorktree(ctx context.Context, wt domain.Worktree, name string, args ...string) (ExecResult, error) {
	if wt.IsMain {
		return ExecResult{}, domain.InvalidInputf("refusing to run a command in the main working tree")
	}
	if wt.Path == "" {
		return ExecResult{}, domain.InvalidInputf("worktree has no path")
	}
	return run(ctx, wt.Path, name, args...)
}

func (g *Git) Remove(ctx context.Context, wt domain.Worktree, force bool) error {
	if wt.IsMain {
		return domain.InvalidInputf("refusing to remove the main working tree")
	}
	if !g.underRoot(wt.Path) {
		return domain.InvalidInputf("refusing to remove worktree outside %s: %s", g.opts.Root, wt.Path)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, wt.Path)
	res, err := g.git(ctx, args...)
	if err != nil {
		return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(res.Stderr))
	}
	g.log.Info().Str("path", wt.Path).Msg("removed worktree")
	return nil
}

func (g *Git) Prune(ctx context.Context, removeBranches bool) ([]domain.Worktree, error) {
	before, err := g.List(ctx)
	if err != nil {
		return nil, err
	}
	if res, err := g.git(ctx, "worktree", "prune"); err != nil {
		return nil, fmt.Errorf("git worktree prune: %w: %s", err, strings.TrimSpace(res.Stderr))
	}
	after, err := g.List(ctx)
	if err != nil {
		return nil, err
	}
	stillThere := make(map[string]bool, len(after))
	for _, wt := range after {
		stillThere[wt.Path] = true
	}
	var removed []domain.Worktree
	for _, wt := range before {
		if wt.IsMain || stillThere[wt.Path] {
			continue
		}
		removed = append(removed, wt)
		if removeBranches && wt.Branch != "" {
			if _, derr := g.git(ctx, "branch", "-D", wt.Branch); derr != nil {
				g.log.Warn().Str("branch", wt.Branch).Err(derr).Msg("could not delete branch")
			}
		}
	}
	return removed, nil
}

func (g *Git) exists(ctx context.Context, full, branch string) bool {
	if _, err := os.Stat(full); err == nil {
		return true
	}
	_, err := g.git(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func (g *Git) underRoot(p string) bool {
	rel, err := filepath.Rel(g.opts.Root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
