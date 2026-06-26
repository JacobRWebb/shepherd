package session

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// nativeBackend runs each agent as a detached OS process whose stdout/stderr are
// captured to a per-session log file. Liveness is derived from the OS process
// table plus an exit-code sentinel file, so sessions survive the shepherd
// process exiting.
type nativeBackend struct {
	store  *Store
	logDir string
	log    *zerolog.Logger
}

var _ SessionBackend = (*nativeBackend)(nil)

func newNative(store *Store, logDir string, log *zerolog.Logger) *nativeBackend {
	if log == nil {
		l := zerolog.Nop()
		log = &l
	}
	return &nativeBackend{store: store, logDir: logDir, log: log}
}

func (n *nativeBackend) Kind() Backend { return BackendNative }

// buildDetached builds the command. The process is NOT tied to a context so it
// outlives the invoking command; lifecycle is managed via PID + Stop.
func buildDetached(spec Spec) *exec.Cmd {
	if runtime.GOOS == "windows" {
		low := strings.ToLower(spec.Program)
		if strings.HasSuffix(low, ".cmd") || strings.HasSuffix(low, ".bat") {
			return exec.Command("cmd.exe", append([]string{"/c", spec.Program}, spec.Args...)...)
		}
	}
	return exec.Command(spec.Program, spec.Args...)
}

func (n *nativeBackend) Start(ctx context.Context, spec Spec) (Info, error) {
	if spec.Name == "" {
		return Info{}, fmt.Errorf("session name required")
	}
	if existing, err := n.store.Get(spec.Name); err == nil {
		if n.reconcile(existing).State == StateRunning {
			return Info{}, ErrExists
		}
	}
	if err := os.MkdirAll(n.logDir, 0o755); err != nil {
		return Info{}, err
	}
	logPath := filepath.Join(n.logDir, sanitizeName(spec.Name)+".log")
	exitPath := logPath + ".exit"
	_ = os.Remove(exitPath)

	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = logf.Close() }()

	cmd := buildDetached(spec)
	cmd.Dir = spec.Dir
	cmd.Env = mergeEnv(spec.Env)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	configureDetached(cmd)

	if err := cmd.Start(); err != nil {
		return Info{}, fmt.Errorf("starting %s: %w", spec.Program, err)
	}
	pid := cmd.Process.Pid

	// Supervise to capture the exit code while shepherd is alive. If shepherd
	// exits first, the detached child keeps running and reconcile() later marks
	// it exited (without a code) once the PID is gone.
	name := spec.Name
	go func() {
		werr := cmd.Wait()
		code := 0
		if werr != nil {
			code = exitCodeOf(werr)
		}
		_ = os.WriteFile(exitPath, []byte(strconv.Itoa(code)), 0o644)
		_ = n.store.Patch(name, func(i *Info) {
			if i.State != StateStopped {
				i.State = StateExited
				i.ExitCode = &code
			}
		})
	}()

	now := time.Now().UTC()
	info := Info{
		Name: name, Backend: BackendNative, State: StateRunning, Dir: spec.Dir,
		PID: pid, LogPath: logPath, StartedAt: now, UpdatedAt: now, Labels: spec.Labels,
	}
	if err := n.store.Upsert(info); err != nil {
		return info, err
	}
	n.log.Info().Str("name", name).Int("pid", pid).Str("log", logPath).Msg("started native session")
	return info, nil
}

func (n *nativeBackend) reconcile(i Info) Info {
	if i.State == StateStopped || i.State == StateExited {
		return i
	}
	if b, err := os.ReadFile(i.LogPath + ".exit"); err == nil {
		code, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		i.State = StateExited
		i.ExitCode = &code
		return i
	}
	if processAlive(i.PID) {
		i.State = StateRunning
	} else {
		i.State = StateExited
	}
	return i
}

func (n *nativeBackend) List(ctx context.Context) ([]Info, error) {
	all, err := n.store.All()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(all))
	for _, i := range all {
		ri := i
		if i.Backend == BackendNative {
			ri = n.reconcile(i)
			if ri.State != i.State || (ri.ExitCode != nil && i.ExitCode == nil) {
				_ = n.store.Upsert(ri)
			}
		}
		out = append(out, ri)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.Before(out[b].StartedAt) })
	return out, nil
}

func (n *nativeBackend) Get(ctx context.Context, name string) (Info, error) {
	i, err := n.store.Get(name)
	if err != nil {
		return Info{}, err
	}
	return n.reconcile(i), nil
}

func (n *nativeBackend) Attach(ctx context.Context, name string) error { return ErrAttachUnsupported }

func (n *nativeBackend) SendInput(ctx context.Context, name, text string, enter bool) error {
	return ErrAttachUnsupported
}

func (n *nativeBackend) Tail(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	i, err := n.store.Get(name)
	if err != nil {
		return nil, err
	}
	return newTailReader(ctx, i.LogPath, follow)
}

func (n *nativeBackend) Snapshot(ctx context.Context, name string, lines int) (string, error) {
	i, err := n.store.Get(name)
	if err != nil {
		return "", err
	}
	return lastLines(i.LogPath, lines)
}

func (n *nativeBackend) Stop(ctx context.Context, name string, force bool) error {
	i, err := n.store.Get(name)
	if err != nil {
		return err
	}
	if i.PID > 0 && processAlive(i.PID) {
		if kerr := killTree(i.PID, force); kerr != nil {
			n.log.Warn().Err(kerr).Int("pid", i.PID).Msg("kill failed")
		}
	}
	return n.store.Patch(name, func(x *Info) { x.State = StateStopped })
}

func (n *nativeBackend) Remove(ctx context.Context, name string, purge bool) error {
	if purge {
		if i, err := n.store.Get(name); err == nil && i.LogPath != "" {
			_ = os.Remove(i.LogPath)
			_ = os.Remove(i.LogPath + ".exit")
		}
	}
	return n.store.Delete(name)
}
