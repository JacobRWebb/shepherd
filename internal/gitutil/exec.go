package gitutil

import (
	"context"
	"os/exec"
)

// commandContext centralizes git process creation. exec.CommandContext resolves
// "git" via PATH (PATHEXT-aware on Windows, so git.exe is found).
func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
