package gitutil

import (
	"context"
	"os/exec"

	"github.com/JacobRWebb/shepherd/internal/sysproc"
)

// commandContext centralizes git process creation. exec.CommandContext resolves
// "git" via PATH (PATHEXT-aware on Windows, so git.exe is found). sysproc.Hide
// keeps the child from flashing a console window on Windows.
func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	sysproc.Hide(cmd)
	return cmd
}
