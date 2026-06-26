// Package gitutil provides small read-only helpers over the git CLI for
// repository metadata (root, current/default branch, cleanliness).
package gitutil

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

func run(ctx context.Context, dir string, args ...string) (stdout, stderr string, err error) {
	cmd := commandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// RepoRoot returns the absolute toplevel of the work tree containing dir (cwd if
// dir is ""). Returns ErrNotGitRepo when not inside a git work tree.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, _, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", domain.ErrNotGitRepo
	}
	return filepath.Clean(strings.TrimSpace(out)), nil
}

// CurrentBranch returns the checked-out branch name (or "HEAD" when detached).
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, errs, err := run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %v: %s", err, strings.TrimSpace(errs))
	}
	return strings.TrimSpace(out), nil
}

// IsClean reports whether the work tree at dir has no uncommitted changes.
func IsClean(ctx context.Context, dir string) (bool, error) {
	out, errs, err := run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %v: %s", err, strings.TrimSpace(errs))
	}
	return strings.TrimSpace(out) == "", nil
}

// DefaultBranch resolves the repository's default branch, preferring
// origin/HEAD, then a local main/master, then the current branch.
func DefaultBranch(ctx context.Context, dir string) (string, error) {
	if out, _, err := run(ctx, dir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(out) // refs/remotes/origin/main
		if b := strings.TrimPrefix(ref, "refs/remotes/origin/"); b != "" && b != ref {
			return b, nil
		}
	}
	for _, b := range []string{"main", "master"} {
		if _, _, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err == nil {
			return b, nil
		}
	}
	return CurrentBranch(ctx, dir)
}

// Exec runs an arbitrary git command in dir, returning combined output. Use for
// mutating operations (add/commit/push) where the caller wants the message.
func Exec(ctx context.Context, dir string, args ...string) (string, error) {
	out, errs, err := run(ctx, dir, args...)
	return strings.TrimSpace(out + errs), err
}

// RemoteURL returns the URL of the named remote (e.g. "origin").
func RemoteURL(ctx context.Context, dir, remote string) (string, error) {
	out, errs, err := run(ctx, dir, "remote", "get-url", remote)
	if err != nil {
		return "", fmt.Errorf("git remote get-url %s: %v: %s", remote, err, strings.TrimSpace(errs))
	}
	return strings.TrimSpace(out), nil
}

// HasRemote reports whether the named remote exists.
func HasRemote(ctx context.Context, dir, remote string) bool {
	out, _, err := run(ctx, dir, "remote")
	if err != nil {
		return false
	}
	for _, r := range strings.Fields(out) {
		if r == remote {
			return true
		}
	}
	return false
}
