package worktree

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/JacobRWebb/shepherd/internal/sysproc"
)

// ExecResult is the captured outcome of a command run in (or for) a worktree.
type ExecResult struct {
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
}

// run executes name+args with cwd=dir, capturing stdout/stderr. A non-zero exit
// returns a populated ExecResult and a non-nil error.
func run(ctx context.Context, dir, name string, args ...string) (ExecResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	sysproc.Hide(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start)}
	if err != nil {
		res.ExitCode = exitCode(err)
	}
	return res, err
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
