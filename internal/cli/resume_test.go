package cli

import (
	"testing"
)

func TestNewResumeCmd_Shape(t *testing.T) {
	cmd := newResumeCmd()

	if cmd.Use != "resume <worktree-or-branch> [extra prompt...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}

	// Requires at least the worktree/branch argument.
	if err := cmd.Args(cmd, nil); err == nil {
		t.Errorf("expected error with no args")
	}
	if err := cmd.Args(cmd, []string{"feature-x"}); err != nil {
		t.Errorf("unexpected error with one arg: %v", err)
	}

	wantFlags := []struct {
		name string
		def  string
	}{
		{"headless", "false"},
		{"session", ""},
		{"fork", "false"},
		{"model", ""},
		{"permission-mode", ""},
		{"prompt", ""},
	}
	for _, wf := range wantFlags {
		f := cmd.Flags().Lookup(wf.name)
		if f == nil {
			t.Errorf("flag %q not registered", wf.name)
			continue
		}
		if f.DefValue != wf.def {
			t.Errorf("flag %q default = %q, want %q", wf.name, f.DefValue, wf.def)
		}
	}
}
