// Package app is the composition root: it resolves the repo, paths, and config
// into the concrete managers (worktree, session backend, agent launcher, forge)
// that the CLI commands consume.
package app

import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/agent"
	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/forge"
	"github.com/JacobRWebb/shepherd/internal/gitutil"
	"github.com/JacobRWebb/shepherd/internal/paths"
	"github.com/JacobRWebb/shepherd/internal/session"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// App holds the wired-up managers for a repository.
type App struct {
	Cfg       config.Config
	Log       *zerolog.Logger
	Paths     paths.Paths
	Repo      domain.Repo
	Worktrees worktree.Manager
	Sessions  session.SessionBackend
	Store     *session.Store

	agentLauncher agent.Launcher
	agentErr      error
	forgeImpl     forge.Forge
	forgeErr      error
}

// New builds the App for the repository containing the working directory.
// Returns domain.ErrNotGitRepo when not inside a git work tree. The agent and
// forge are constructed best-effort: errors are deferred to Agent()/Forge() so
// commands that don't need them (init, status) still work.
func New(ctx context.Context, cfg config.Config, log *zerolog.Logger) (*App, error) {
	root, err := gitutil.RepoRoot(ctx, "")
	if err != nil {
		return nil, err
	}
	p := paths.Resolve(root, cfg.Worktrees.Root, cfg.Session.Native.LogDir)
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}

	base := cfg.Worktrees.BaseBranch
	if base == "" {
		if db, derr := gitutil.DefaultBranch(ctx, root); derr == nil {
			base = db
		}
	}

	wt := worktree.NewGit(worktree.Options{
		RepoRoot:     root,
		Root:         p.WorktreesRoot,
		NameTemplate: cfg.Worktrees.NameTemplate,
		BranchPrefix: cfg.Worktrees.BranchPrefix,
		DefaultBase:  base,
	}, log)

	store, err := session.OpenStore(p.SessionsFile)
	if err != nil {
		return nil, err
	}
	be, err := session.Detect(session.DetectOptions{
		Mode:       cfg.Session.Backend,
		Store:      store,
		LogDir:     p.LogDir,
		TmuxSocket: cfg.Session.Tmux.SocketName,
		TmuxPrefix: cfg.Session.Tmux.SessionPrefix,
		Log:        log,
	})
	if err != nil {
		return nil, err
	}

	a := &App{
		Cfg:       cfg,
		Log:       log,
		Paths:     p,
		Repo:      detectRepo(ctx, root, cfg),
		Worktrees: wt,
		Sessions:  be,
		Store:     store,
	}
	a.agentLauncher, a.agentErr = agent.NewClaude(cfg.Claude, log)
	a.forgeImpl, a.forgeErr = forge.Select(cfg.Forge)
	return a, nil
}

// Agent returns the claude launcher, or the construction error.
func (a *App) Agent() (agent.Launcher, error) {
	if a.agentLauncher == nil {
		return nil, a.agentErr
	}
	return a.agentLauncher, nil
}

// Forge returns the configured forge, or the construction error.
func (a *App) Forge() (forge.Forge, error) {
	if a.forgeImpl == nil {
		if a.forgeErr != nil {
			return nil, a.forgeErr
		}
		return nil, domain.InvalidInputf("no forge available")
	}
	return a.forgeImpl, nil
}

func detectRepo(ctx context.Context, root string, cfg config.Config) domain.Repo {
	if cfg.Forge.Provider == "bitbucket" {
		return domain.Repo{Owner: cfg.Forge.Bitbucket.Workspace, Name: cfg.Forge.Bitbucket.RepoSlug}
	}
	url, err := gitutil.RemoteURL(ctx, root, "origin")
	if err != nil {
		return domain.Repo{}
	}
	return parseRemoteRepo(url)
}

// parseRemoteRepo extracts owner/name from common git remote URL shapes
// (git@host:owner/repo.git, https://host/owner/repo.git).
func parseRemoteRepo(url string) domain.Repo {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
	}
	if i := strings.LastIndex(url, "@"); i >= 0 {
		url = url[i+1:]
	}
	url = strings.ReplaceAll(url, ":", "/")
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return domain.Repo{Owner: parts[len(parts)-2], Name: parts[len(parts)-1]}
	}
	return domain.Repo{}
}
