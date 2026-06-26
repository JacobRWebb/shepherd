// Package paths resolves the on-disk locations Shepherd uses for a repository:
// the runtime state directory (.shepherd), native session logs, the session
// registry file, and the worktrees root (which may live outside the repo).
package paths

import (
	"os"
	"path/filepath"
)

// Paths holds resolved absolute locations for a repo.
type Paths struct {
	RepoRoot      string // absolute repo root (main work tree)
	StateDir      string // <repo>/.shepherd
	LogDir        string // native session logs
	SessionsFile  string // <repo>/.shepherd/sessions.json
	WorktreesRoot string // resolved worktrees root (may be outside the repo)
}

// Resolve computes Paths. worktreesRoot and nativeLogDir may be relative, in
// which case they resolve against repoRoot.
func Resolve(repoRoot, worktreesRoot, nativeLogDir string) Paths {
	state := filepath.Join(repoRoot, ".shepherd")

	wr := worktreesRoot
	if wr == "" {
		wr = "../.shepherd-worktrees"
	}
	if !filepath.IsAbs(wr) {
		wr = filepath.Join(repoRoot, wr)
	}

	logDir := nativeLogDir
	if logDir == "" {
		logDir = filepath.Join(state, "logs")
	} else if !filepath.IsAbs(logDir) {
		logDir = filepath.Join(repoRoot, logDir)
	}

	return Paths{
		RepoRoot:      filepath.Clean(repoRoot),
		StateDir:      filepath.Clean(state),
		LogDir:        filepath.Clean(logDir),
		SessionsFile:  filepath.Join(state, "sessions.json"),
		WorktreesRoot: filepath.Clean(wr),
	}
}

// EnsureDirs creates the state, log, and worktrees-root directories.
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.StateDir, p.LogDir, p.WorktreesRoot} {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
