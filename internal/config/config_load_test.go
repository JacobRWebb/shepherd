package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLayering(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".shepherd.yaml")
	content := "forge:\n  provider: bitbucket\n  bitbucket:\n    workspace: ws\nclaude:\n  model: sonnet\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// env overrides the file value
	t.Setenv("SHEPHERD_CLAUDE__MODEL", "opus")

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Forge.Provider != "bitbucket" {
		t.Errorf("file value not applied: provider = %q", cfg.Forge.Provider)
	}
	if cfg.Claude.Model != "opus" {
		t.Errorf("env override not applied: model = %q", cfg.Claude.Model)
	}
	if cfg.Worktrees.BranchPrefix != "shepherd/" {
		t.Errorf("default lost: branch_prefix = %q", cfg.Worktrees.BranchPrefix)
	}
}

func TestLoadMissingExplicitPath(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Errorf("expected an error for a missing explicit --config path")
	}
}
