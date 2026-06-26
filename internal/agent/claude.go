// Package agent launches the `claude` CLI inside a worktree, either interactively
// (inheriting the terminal) or headlessly (`claude -p`, capturing output). The
// Launcher interface is consumed by the cli/crew/session layers; Claude is the
// implementation.
package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// resolveBinary finds the claude executable. An explicit path (containing a
// separator) is trusted as-is; a bare name is looked up on PATH (PATHEXT-aware
// on Windows, so claude.exe / claude.cmd are found).
func resolveBinary(binary string) (string, error) {
	if binary == "" {
		binary = "claude"
	}
	if strings.ContainsAny(binary, `/\`) {
		return binary, nil
	}
	p, err := exec.LookPath(binary)
	if err != nil {
		return "", domain.NotFoundf("claude binary %q not found on PATH (install Claude Code or set claude.binary)", binary)
	}
	return p, nil
}

// command builds a context-bound *exec.Cmd for the resolved binary. On Windows a
// .cmd/.bat shim is executed through cmd.exe; a real .exe is invoked directly.
func command(ctx context.Context, bin string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" && isShim(bin) {
		return exec.CommandContext(ctx, "cmd.exe", append([]string{"/c", bin}, args...)...)
	}
	return exec.CommandContext(ctx, bin, args...)
}

// commandPlain is like command but NOT bound to a context, used for interactive
// runs so the process is controlled by the user's terminal (Ctrl-C reaches
// claude directly) rather than being killed when a request context cancels.
func commandPlain(bin string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" && isShim(bin) {
		return exec.Command("cmd.exe", append([]string{"/c", bin}, args...)...)
	}
	return exec.Command(bin, args...)
}

func isShim(bin string) bool {
	low := strings.ToLower(bin)
	return strings.HasSuffix(low, ".cmd") || strings.HasSuffix(low, ".bat")
}

// ProbeVersion returns the output of `claude --version`.
func ProbeVersion(ctx context.Context, binary string) (string, error) {
	bin, err := resolveBinary(binary)
	if err != nil {
		return "", err
	}
	out, err := command(ctx, bin, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
