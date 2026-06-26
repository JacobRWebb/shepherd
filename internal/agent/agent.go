package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
)

// Spec holds the config-derived knobs common to both modes. Zero values fall
// back to the ClaudeConfig defaults.
type Spec struct {
	Prompt             string   // positional prompt / task instructions
	Model              string   // --model
	PermissionMode     string   // --permission-mode
	SkipPermissions    bool     // --dangerously-skip-permissions
	Effort             string   // --effort
	AddDirs            []string // --add-dir (worktree dir is the cwd, so usually unnecessary)
	AppendSystemPrompt string   // --append-system-prompt
	AllowedTools       []string // --allowed-tools (joined space-separated)
	DisallowedTools    []string // --disallowed-tools
	ContinueSession    bool     // -c/--continue
	ResumeSessionID    string   // -r/--resume <id>
	SessionID          string   // --session-id <uuid>
	ForkSession        bool     // --fork-session
	ExtraArgs          []string // raw passthrough
}

// InteractiveSpec configures an interactive run.
type InteractiveSpec struct{ Spec }

// HeadlessSpec configures a headless (`-p`) run.
type HeadlessSpec struct {
	Spec
	OutputFormat  string            // text|json|stream-json
	FallbackModel string            // --fallback-model
	MaxBudgetUSD  float64           // --max-budget-usd
	Timeout       time.Duration     // 0 => ClaudeConfig.Headless.TimeoutSeconds
	StreamHandler func(StreamEvent) // optional; only for stream-json
}

// Launcher runs claude.
type Launcher interface {
	Interactive(ctx context.Context, wt domain.Worktree, spec InteractiveSpec) (exitCode int, err error)
	Headless(ctx context.Context, wt domain.Worktree, spec HeadlessSpec) (HeadlessResult, error)
	// InteractiveArgs / HeadlessArgs return the exact argv Shepherd would run
	// (used by the session backend to host the process).
	InteractiveArgs(spec InteractiveSpec) []string
	HeadlessArgs(spec HeadlessSpec) []string
	// Binary returns the resolved claude executable path.
	Binary() string
}

// Claude is the Launcher backed by the claude CLI.
type Claude struct {
	cfg config.ClaudeConfig
	bin string
	log *zerolog.Logger
}

var _ Launcher = (*Claude)(nil)

// NewClaude resolves the claude binary and returns a Launcher.
func NewClaude(cfg config.ClaudeConfig, log *zerolog.Logger) (*Claude, error) {
	bin, err := resolveBinary(cfg.Binary)
	if err != nil {
		return nil, err
	}
	if log == nil {
		l := zerolog.Nop()
		log = &l
	}
	return &Claude{cfg: cfg, bin: bin, log: log}, nil
}

func (c *Claude) Binary() string { return c.bin }

// Interactive runs claude attached to the current terminal (stdio inherited),
// with cwd = wt.Path. Blocks until claude exits; returns its exit code.
func (c *Claude) Interactive(ctx context.Context, wt domain.Worktree, spec InteractiveSpec) (int, error) {
	dir := wt.Path
	if dir == "" {
		return -1, domain.InvalidInputf("interactive run requires a worktree path")
	}
	argv := c.InteractiveArgs(spec)
	// Not context-bound: the interactive session is driven by the terminal, so
	// the user's Ctrl-C reaches claude directly instead of force-killing it.
	cmd := commandPlain(c.bin, argv...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	c.log.Debug().Strs("argv", argv).Str("dir", dir).Msg("launching interactive claude")
	err := cmd.Run()
	return exitCodeFrom(err), execError(err)
}

// Headless runs `claude -p` with cwd = wt.Path and captures the result.
func (c *Claude) Headless(ctx context.Context, wt domain.Worktree, spec HeadlessSpec) (HeadlessResult, error) {
	dir := wt.Path
	if dir == "" {
		return HeadlessResult{}, domain.InvalidInputf("headless run requires a worktree path")
	}

	timeout := spec.Timeout
	if timeout <= 0 && c.cfg.Headless.TimeoutSeconds > 0 {
		timeout = time.Duration(c.cfg.Headless.TimeoutSeconds) * time.Second
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	format := pick(spec.OutputFormat, c.cfg.Headless.OutputFormat)
	if format == "" {
		format = "json"
	}
	spec.OutputFormat = format
	argv := c.HeadlessArgs(spec)

	cmd := command(ctx, c.bin, argv...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	c.log.Debug().Strs("argv", argv).Str("dir", dir).Msg("launching headless claude")

	if format == "stream-json" {
		return c.runStream(ctx, cmd, spec.StreamHandler)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return HeadlessResult{ExitCode: -1, Stderr: stderr.String()}, fmt.Errorf("claude headless run timed out after %s", timeout)
	}
	res := parseHeadless(stdout.Bytes(), format)
	res.ExitCode = exitCodeFrom(err)
	res.Stderr = trimSpace(stderr.String())
	if res.Text == "" && res.Stderr != "" {
		res.Text = res.Stderr
	}
	return res, execError(err)
}

func (c *Claude) runStream(ctx context.Context, cmd *exec.Cmd, handler func(StreamEvent)) (HeadlessResult, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return HeadlessResult{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return HeadlessResult{}, err
	}

	var final HeadlessResult
	sc := bufio.NewScanner(stdoutPipe)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(line, &probe)
		if handler != nil {
			handler(StreamEvent{Type: probe.Type, Raw: append(json.RawMessage(nil), line...)})
		}
		if probe.Type == "result" {
			final = parseHeadless(line, "json")
		}
	}
	waitErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		final.ExitCode = -1
		final.Stderr = trimSpace(stderr.String())
		return final, fmt.Errorf("claude headless run timed out")
	}
	final.ExitCode = exitCodeFrom(waitErr)
	final.Stderr = trimSpace(stderr.String())
	return final, execError(waitErr)
}

func exitCodeFrom(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// execError returns nil for a plain non-zero exit (the caller inspects ExitCode)
// and the real error for an exec failure (binary missing, context cancelled).
func execError(err error) error {
	if err == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return nil
	}
	return err
}
