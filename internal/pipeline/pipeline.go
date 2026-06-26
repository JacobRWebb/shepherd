// Package pipeline runs the configurable validation steps inside a worktree and
// implements the "clean push gate": a push is allowed only when every required
// step passes. It executes shell commands and never pushes anything itself.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
)

// Step is one validation command.
type Step struct {
	Name            string
	Run             string
	Timeout         time.Duration
	ContinueOnError bool // failure is recorded but does NOT block the gate
	WorkdirRel      string
	Env             map[string]string
}

// Config is the runtime validation configuration.
type Config struct {
	StopOnFailure  bool
	DefaultTimeout time.Duration
	Env            map[string]string
	Steps          []Step
}

// FromConfig converts the YAML-facing config into a runtime Config, parsing
// duration strings.
func FromConfig(c config.ValidationConfig) (Config, error) {
	out := Config{StopOnFailure: c.StopOnFailure, Env: c.Env}
	if c.DefaultTimeout != "" {
		d, err := time.ParseDuration(c.DefaultTimeout)
		if err != nil {
			return Config{}, domain.InvalidInputf("validation.default_timeout: %v", err)
		}
		out.DefaultTimeout = d
	}
	for i, s := range c.Steps {
		step := Step{Name: s.Name, Run: s.Run, ContinueOnError: s.ContinueOnError, WorkdirRel: s.WorkdirRel}
		if step.Name == "" {
			step.Name = fmt.Sprintf("step-%d", i+1)
		}
		if s.Timeout != "" {
			d, err := time.ParseDuration(s.Timeout)
			if err != nil {
				return Config{}, domain.InvalidInputf("validation.steps[%d].timeout: %v", i, err)
			}
			step.Timeout = d
		}
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

// StepStatus is the outcome of a single step.
type StepStatus string

const (
	StepPassed   StepStatus = "passed"
	StepFailed   StepStatus = "failed"
	StepTimedOut StepStatus = "timed_out"
	StepSkipped  StepStatus = "skipped"
)

// StepResult captures one step's execution.
type StepResult struct {
	Name     string        `json:"name"`
	Status   StepStatus    `json:"status"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout,omitempty"`
	Stderr   string        `json:"stderr,omitempty"`
	Duration time.Duration `json:"duration"`
	Advisory bool          `json:"advisory,omitempty"` // continue_on_error step
}

// Result is the aggregate pipeline outcome.
type Result struct {
	Steps    []StepResult  `json:"steps"`
	Passed   bool          `json:"passed"`
	Failed   []StepResult  `json:"failed,omitempty"`
	Duration time.Duration `json:"duration"`
}

// FailureDigest renders a compact summary of failed steps for an agent to fix.
func (r Result) FailureDigest() string {
	var b strings.Builder
	for _, s := range r.Failed {
		fmt.Fprintf(&b, "### step %q (%s, exit %d)\n", s.Name, s.Status, s.ExitCode)
		if out := strings.TrimSpace(s.Stdout); out != "" {
			b.WriteString("stdout:\n" + tail(out, 2000) + "\n")
		}
		if errs := strings.TrimSpace(s.Stderr); errs != "" {
			b.WriteString("stderr:\n" + tail(errs, 2000) + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Runner executes validation pipelines.
type Runner struct {
	cfg Config
	log *zerolog.Logger
}

// NewRunner builds a Runner.
func NewRunner(cfg Config, log *zerolog.Logger) *Runner {
	if log == nil {
		l := zerolog.Nop()
		log = &l
	}
	return &Runner{cfg: cfg, log: log}
}

// Run executes all steps in dir and returns the aggregate result.
func (r *Runner) Run(ctx context.Context, dir string) (Result, error) {
	start := time.Now()
	var res Result
	stopped := false
	for i, step := range r.cfg.Steps {
		if stopped {
			res.Steps = append(res.Steps, StepResult{Name: step.Name, Status: StepSkipped, Advisory: step.ContinueOnError})
			continue
		}
		sr := r.runStep(ctx, dir, step)
		res.Steps = append(res.Steps, sr)
		failed := sr.Status == StepFailed || sr.Status == StepTimedOut
		if failed && !step.ContinueOnError {
			res.Failed = append(res.Failed, sr)
			if r.cfg.StopOnFailure {
				stopped = true
			}
		}
		_ = i
	}
	res.Duration = time.Since(start)
	res.Passed = len(res.Failed) == 0
	return res, nil
}

// Gate runs the pipeline and reports whether a push is allowed. It never pushes.
func (r *Runner) Gate(ctx context.Context, dir string) (allowed bool, result Result, err error) {
	res, err := r.Run(ctx, dir)
	if err != nil {
		return false, res, err
	}
	return res.Passed, res, nil
}

func (r *Runner) runStep(ctx context.Context, dir string, step Step) StepResult {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = r.cfg.DefaultTimeout
	}
	cctx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	name, args := shellCommand(step.Run)
	cmd := exec.CommandContext(cctx, name, args...)
	wd := dir
	if step.WorkdirRel != "" {
		wd = filepath.Join(dir, step.WorkdirRel)
	}
	cmd.Dir = wd
	cmd.Env = mergedEnv(r.cfg.Env, step.Env)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	r.log.Debug().Str("step", step.Name).Str("run", step.Run).Str("dir", wd).Msg("pipeline step")
	err := cmd.Run()
	dur := time.Since(start)

	sr := StepResult{Name: step.Name, Stdout: stdout.String(), Stderr: stderr.String(), Duration: dur, Advisory: step.ContinueOnError}
	switch {
	case cctx.Err() == context.DeadlineExceeded:
		sr.Status = StepTimedOut
		sr.ExitCode = -1
	case err == nil:
		sr.Status = StepPassed
	default:
		sr.Status = StepFailed
		sr.ExitCode = exitCode(err)
	}
	return sr
}

func shellCommand(line string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", line}
	}
	return "sh", []string{"-c", line}
}

func mergedEnv(base, extra map[string]string) []string {
	env := os.Environ()
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "...\n" + s[len(s)-n:]
}
