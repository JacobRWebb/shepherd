package gitutil

import (
	"context"
	"os/exec"
	"testing"
)

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestRepoHelpers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	ctx := context.Background()

	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Tester")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "init")

	if root, err := RepoRoot(ctx, dir); err != nil || root == "" {
		t.Errorf("RepoRoot = %q err = %v", root, err)
	}
	if br, err := CurrentBranch(ctx, dir); err != nil || br != "main" {
		t.Errorf("CurrentBranch = %q err = %v", br, err)
	}
	if clean, err := IsClean(ctx, dir); err != nil || !clean {
		t.Errorf("IsClean = %v err = %v", clean, err)
	}
}

func TestRepoRootNotARepo(t *testing.T) {
	if _, err := RepoRoot(context.Background(), t.TempDir()); err == nil {
		t.Errorf("expected ErrNotGitRepo outside a repository")
	}
}
