package session

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// tmuxBackend runs each agent in a detached tmux session whose pane output is
// piped to a log file. It supports live attach and input, unlike native.
type tmuxBackend struct {
	store  *Store
	socket string
	prefix string
	logDir string
	log    *zerolog.Logger
}

var _ SessionBackend = (*tmuxBackend)(nil)

func newTmux(store *Store, socket, prefix, logDir string, log *zerolog.Logger) *tmuxBackend {
	if log == nil {
		l := zerolog.Nop()
		log = &l
	}
	if prefix == "" {
		prefix = "shp-"
	}
	return &tmuxBackend{store: store, socket: socket, prefix: prefix, logDir: logDir, log: log}
}

func (t *tmuxBackend) Kind() Backend { return BackendTmux }

func (t *tmuxBackend) full(name string) string { return t.prefix + name }

func (t *tmuxBackend) tmux(ctx context.Context, args ...string) (string, string, error) {
	if t.socket != "" {
		args = append([]string{"-L", t.socket}, args...)
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildShellScript wraps the program so env is exported, then prints an exit
// sentinel the reconciler can read from the captured log.
func buildShellScript(spec Spec, _ string) string {
	var b strings.Builder
	for k, v := range spec.Env {
		b.WriteString("export " + k + "=" + shellQuote(v) + "; ")
	}
	b.WriteString(shellQuote(spec.Program))
	for _, a := range spec.Args {
		b.WriteString(" " + shellQuote(a))
	}
	b.WriteString(`; code=$?; echo "[[SHEP_EXIT:$code]]"; exec sleep 1`)
	return b.String()
}

func (t *tmuxBackend) Start(ctx context.Context, spec Spec) (Info, error) {
	if spec.Name == "" {
		return Info{}, fmt.Errorf("session name required")
	}
	full := t.full(spec.Name)
	if _, _, err := t.tmux(ctx, "has-session", "-t", full); err == nil {
		return Info{}, ErrExists
	}
	if err := os.MkdirAll(t.logDir, 0o755); err != nil {
		return Info{}, err
	}
	logPath := filepath.Join(t.logDir, sanitizeName(spec.Name)+".log")
	_ = os.Remove(logPath)

	inner := buildShellScript(spec, logPath)
	out, errs, err := t.tmux(ctx, "new-session", "-d", "-s", full, "-c", spec.Dir,
		"-x", "220", "-y", "50", "-P", "-F", "#{session_name}\t#{pane_pid}",
		"sh", "-lc", inner)
	if err != nil {
		return Info{}, fmt.Errorf("tmux new-session: %v: %s", err, strings.TrimSpace(errs))
	}
	pid := 0
	if parts := strings.Split(strings.TrimSpace(out), "\t"); len(parts) == 2 {
		pid, _ = strconv.Atoi(parts[1])
	}
	if _, perr, err := t.tmux(ctx, "pipe-pane", "-o", "-t", full, "cat >> "+shellQuote(logPath)); err != nil {
		t.log.Warn().Str("err", strings.TrimSpace(perr)).Msg("tmux pipe-pane failed")
	}

	now := time.Now().UTC()
	info := Info{
		Name: spec.Name, Backend: BackendTmux, State: StateRunning, Dir: spec.Dir,
		PID: pid, TmuxName: full, LogPath: logPath, StartedAt: now, UpdatedAt: now, Labels: spec.Labels,
	}
	if err := t.store.Upsert(info); err != nil {
		return info, err
	}
	t.log.Info().Str("name", spec.Name).Str("tmux", full).Msg("started tmux session")
	return info, nil
}

func (t *tmuxBackend) liveSessions(ctx context.Context) map[string]bool {
	live := map[string]bool{}
	out, _, err := t.tmux(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return live
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			live[s] = true
		}
	}
	return live
}

func (t *tmuxBackend) reconcile(i Info, live map[string]bool) Info {
	if i.State == StateStopped || i.State == StateExited {
		return i
	}
	if live[i.TmuxName] {
		i.State = StateRunning
		return i
	}
	if code, ok := readExitSentinel(i.LogPath); ok {
		i.State = StateExited
		i.ExitCode = &code
	} else {
		i.State = StateExited
	}
	return i
}

func readExitSentinel(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := string(b)
	const marker = "[[SHEP_EXIT:"
	idx := strings.LastIndex(s, marker)
	if idx < 0 {
		return 0, false
	}
	rest := s[idx+len(marker):]
	end := strings.Index(rest, "]]")
	if end < 0 {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0, false
	}
	return code, true
}

func (t *tmuxBackend) List(ctx context.Context) ([]Info, error) {
	all, err := t.store.All()
	if err != nil {
		return nil, err
	}
	live := t.liveSessions(ctx)
	out := make([]Info, 0, len(all))
	for _, i := range all {
		ri := i
		if i.Backend == BackendTmux {
			ri = t.reconcile(i, live)
			if ri.State != i.State {
				_ = t.store.Upsert(ri)
			}
		}
		out = append(out, ri)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.Before(out[b].StartedAt) })
	return out, nil
}

func (t *tmuxBackend) Get(ctx context.Context, name string) (Info, error) {
	i, err := t.store.Get(name)
	if err != nil {
		return Info{}, err
	}
	live := map[string]bool{}
	if _, _, err := t.tmux(ctx, "has-session", "-t", t.full(name)); err == nil {
		live[t.full(name)] = true
	}
	return t.reconcile(i, live), nil
}

func (t *tmuxBackend) Attach(ctx context.Context, name string) error {
	args := []string{}
	if t.socket != "" {
		args = append(args, "-L", t.socket)
	}
	args = append(args, "attach-session", "-t", t.full(name))
	cmd := exec.CommandContext(ctx, "tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (t *tmuxBackend) Tail(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	i, err := t.store.Get(name)
	if err != nil {
		return nil, err
	}
	return newTailReader(ctx, i.LogPath, follow)
}

func (t *tmuxBackend) Snapshot(ctx context.Context, name string, lines int) (string, error) {
	full := t.full(name)
	if out, _, err := t.tmux(ctx, "capture-pane", "-p", "-J", "-S", "-"+strconv.Itoa(lines), "-t", full); err == nil {
		return strings.TrimRight(out, "\n"), nil
	}
	i, err := t.store.Get(name)
	if err != nil {
		return "", err
	}
	return lastLines(i.LogPath, lines)
}

func (t *tmuxBackend) SendInput(ctx context.Context, name, text string, enter bool) error {
	full := t.full(name)
	if _, errs, err := t.tmux(ctx, "send-keys", "-t", full, "-l", "--", text); err != nil {
		return fmt.Errorf("tmux send-keys: %v: %s", err, strings.TrimSpace(errs))
	}
	if enter {
		_, _, _ = t.tmux(ctx, "send-keys", "-t", full, "Enter")
	}
	return nil
}

func (t *tmuxBackend) Stop(ctx context.Context, name string, force bool) error {
	full := t.full(name)
	if !force {
		_, _, _ = t.tmux(ctx, "send-keys", "-t", full, "C-c")
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	_, _, _ = t.tmux(ctx, "kill-session", "-t", full)
	return t.store.Patch(name, func(i *Info) { i.State = StateStopped })
}

func (t *tmuxBackend) Remove(ctx context.Context, name string, purge bool) error {
	if purge {
		if i, err := t.store.Get(name); err == nil && i.LogPath != "" {
			_ = os.Remove(i.LogPath)
		}
	}
	return t.store.Delete(name)
}
