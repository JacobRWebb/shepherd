// Package session hosts agent processes behind a pluggable SessionBackend.
//
// Two backends are provided: native (detached OS processes with per-session log
// files, the default and the only one that works on native Windows) and tmux
// (live attachable panes, for Linux/macOS/WSL). Detect picks one from config.
// Session metadata is persisted to a file-locked JSON registry so a later
// `shepherd` invocation can see sessions started by an earlier one.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/rs/zerolog"
)

// Backend identifies an implementation.
type Backend string

const (
	BackendTmux   Backend = "tmux"
	BackendNative Backend = "native"
)

// State is the lifecycle state of a session.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateExited   State = "exited"  // process ended (see ExitCode)
	StateStopped  State = "stopped" // Shepherd killed it
	StateUnknown  State = "unknown" // backend cannot determine
)

// Spec describes a session to start. Backend-agnostic.
type Spec struct {
	Name    string            // unique session name, e.g. "crew-3f9a-agent-2"
	Dir     string            // working directory (the worktree path)
	Program string            // executable, normally the resolved claude path
	Args    []string          // argv after Program
	Env     map[string]string // extra env merged over the process environment
	Labels  map[string]string // free-form metadata: crew id, task #, branch, claude session id
}

// Info is the observable state of one session.
type Info struct {
	Name      string            `json:"name"`
	Backend   Backend           `json:"backend"`
	State     State             `json:"state"`
	Dir       string            `json:"dir"`
	PID       int               `json:"pid,omitempty"`
	TmuxName  string            `json:"tmux_name,omitempty"`
	LogPath   string            `json:"log_path,omitempty"`
	ExitCode  *int              `json:"exit_code,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Sentinel errors.
var (
	ErrNotFound          = errors.New("session: not found")
	ErrExists            = errors.New("session: already exists")
	ErrAttachUnsupported = errors.New("session: attach not supported by this backend")
)

// SessionBackend is the pluggable abstraction over how agent processes are run.
type SessionBackend interface {
	Kind() Backend

	// Start launches a detached session and persists its metadata.
	Start(ctx context.Context, spec Spec) (Info, error)

	// List returns all sessions, reconciling persisted state against reality.
	List(ctx context.Context) ([]Info, error)

	// Get returns one session (ErrNotFound if absent).
	Get(ctx context.Context, name string) (Info, error)

	// Attach connects the current terminal to the session, blocking until the
	// user detaches. Backends that cannot attach return ErrAttachUnsupported.
	Attach(ctx context.Context, name string) error

	// Tail streams the session's output. follow mirrors `tail -f`.
	Tail(ctx context.Context, name string, follow bool) (io.ReadCloser, error)

	// Snapshot returns the last n lines of output (cheap, for dashboards).
	Snapshot(ctx context.Context, name string, lines int) (string, error)

	// SendInput types text into an interactive session (tmux only).
	SendInput(ctx context.Context, name, text string, enter bool) error

	// Stop terminates the session (graceful then force) and its process tree.
	Stop(ctx context.Context, name string, force bool) error

	// Remove deletes persisted metadata (and logs if purge). Never touches the
	// git worktree.
	Remove(ctx context.Context, name string, purge bool) error
}

// DetectOptions configures backend selection.
type DetectOptions struct {
	Mode       string // auto | tmux | native
	Store      *Store
	LogDir     string // native session logs (and tmux pipe-pane logs)
	TmuxSocket string
	TmuxPrefix string
	Log        *zerolog.Logger
}

// Detect selects a backend. "auto" prefers tmux on non-Windows hosts where tmux
// is on PATH, falling back to native everywhere else.
func Detect(o DetectOptions) (SessionBackend, error) {
	switch o.Mode {
	case "", "auto":
		if runtime.GOOS != "windows" && tmuxAvailable() {
			return newTmux(o.Store, o.TmuxSocket, o.TmuxPrefix, o.LogDir, o.Log), nil
		}
		return newNative(o.Store, o.LogDir, o.Log), nil
	case "native":
		return newNative(o.Store, o.LogDir, o.Log), nil
	case "tmux":
		if !tmuxAvailable() {
			return nil, fmt.Errorf("session backend=tmux but tmux was not found on PATH; use backend=native or auto")
		}
		return newTmux(o.Store, o.TmuxSocket, o.TmuxPrefix, o.LogDir, o.Log), nil
	default:
		return nil, fmt.Errorf("unknown session backend %q (want auto|tmux|native)", o.Mode)
	}
}

func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}
