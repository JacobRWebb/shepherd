package paths

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	p := Resolve(root, "../wt", "")

	if p.StateDir != filepath.Join(root, ".shepherd") {
		t.Errorf("StateDir = %q", p.StateDir)
	}
	if p.WorktreesRoot != filepath.Clean(filepath.Join(root, "../wt")) {
		t.Errorf("WorktreesRoot = %q", p.WorktreesRoot)
	}
	if p.LogDir != filepath.Join(root, ".shepherd", "logs") {
		t.Errorf("LogDir = %q", p.LogDir)
	}
	if filepath.Base(p.SessionsFile) != "sessions.json" {
		t.Errorf("SessionsFile = %q", p.SessionsFile)
	}
}

func TestResolveAbsoluteWorktreesRoot(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "elsewhere")
	p := Resolve(root, abs, "")
	if p.WorktreesRoot != filepath.Clean(abs) {
		t.Errorf("absolute root not preserved: %q", p.WorktreesRoot)
	}
}
