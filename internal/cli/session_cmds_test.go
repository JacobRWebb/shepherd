package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/app"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/output"
	"github.com/JacobRWebb/shepherd/internal/session"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// --- fakes -----------------------------------------------------------------

type stopCall struct {
	name  string
	force bool
}

type removeCall struct {
	name  string
	purge bool
}

type fakeSessions struct {
	snapshot  string
	snapErr   error
	tailData  string
	tailErr   error
	attachErr error
	stopErr   error
	removeErr error

	snapName   string
	snapLines  int
	tailName   string
	tailFollow bool
	attachName string
	stopCalls  []stopCall
	removeCall []removeCall
}

var _ session.SessionBackend = (*fakeSessions)(nil)

func (f *fakeSessions) Kind() session.Backend { return session.BackendNative }

func (f *fakeSessions) Start(context.Context, session.Spec) (session.Info, error) {
	return session.Info{}, nil
}

func (f *fakeSessions) List(context.Context) ([]session.Info, error) { return nil, nil }

func (f *fakeSessions) Get(context.Context, string) (session.Info, error) {
	return session.Info{}, nil
}

func (f *fakeSessions) Attach(_ context.Context, name string) error {
	f.attachName = name
	return f.attachErr
}

func (f *fakeSessions) Tail(_ context.Context, name string, follow bool) (io.ReadCloser, error) {
	f.tailName = name
	f.tailFollow = follow
	if f.tailErr != nil {
		return nil, f.tailErr
	}
	return io.NopCloser(strings.NewReader(f.tailData)), nil
}

func (f *fakeSessions) Snapshot(_ context.Context, name string, lines int) (string, error) {
	f.snapName = name
	f.snapLines = lines
	return f.snapshot, f.snapErr
}

func (f *fakeSessions) SendInput(context.Context, string, string, bool) error { return nil }

func (f *fakeSessions) Stop(_ context.Context, name string, force bool) error {
	f.stopCalls = append(f.stopCalls, stopCall{name: name, force: force})
	return f.stopErr
}

func (f *fakeSessions) Remove(_ context.Context, name string, purge bool) error {
	f.removeCall = append(f.removeCall, removeCall{name: name, purge: purge})
	return f.removeErr
}

type fakeWorktrees struct {
	get       domain.Worktree
	getErr    error
	removeErr error

	removed     []domain.Worktree
	removeForce bool
}

var _ worktree.Manager = (*fakeWorktrees)(nil)

func (f *fakeWorktrees) Create(context.Context, domain.Task, string) (domain.Worktree, error) {
	return domain.Worktree{}, nil
}

func (f *fakeWorktrees) List(context.Context) ([]domain.Worktree, error) { return nil, nil }

func (f *fakeWorktrees) Get(context.Context, string) (domain.Worktree, error) {
	return f.get, f.getErr
}

func (f *fakeWorktrees) RunInWorktree(context.Context, domain.Worktree, string, ...string) (worktree.ExecResult, error) {
	return worktree.ExecResult{}, nil
}

func (f *fakeWorktrees) Remove(_ context.Context, wt domain.Worktree, force bool) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, wt)
	f.removeForce = force
	return nil
}

func (f *fakeWorktrees) Prune(context.Context, bool) ([]domain.Worktree, error) { return nil, nil }

// --- harness ---------------------------------------------------------------

func runCmd(t *testing.T, cmd *cobra.Command, sess session.SessionBackend, wts worktree.Manager, out io.Writer, args ...string) error {
	t.Helper()
	a := &app.App{Sessions: sess, Worktrees: wts}
	st := &cmdState{Out: output.New(false, out), ctx: context.Background()}
	st.appOnce.Do(func() { st.app = a })

	ctx := context.WithValue(context.Background(), ctxKey{}, st)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written. The logs command copies raw session output to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

// --- tests -----------------------------------------------------------------

func TestLogsSnapshot(t *testing.T) {
	sess := &fakeSessions{snapshot: "hello\nworld\n"}
	out := captureStdout(t, func() {
		if err := runCmd(t, newLogsCmd(), sess, &fakeWorktrees{}, io.Discard, "sess-1"); err != nil {
			t.Fatalf("logs: %v", err)
		}
	})
	if sess.snapName != "sess-1" {
		t.Errorf("snapshot name = %q, want sess-1", sess.snapName)
	}
	if sess.snapLines != 200 {
		t.Errorf("snapshot lines = %d, want 200 (default)", sess.snapLines)
	}
	if sess.tailName != "" {
		t.Errorf("Tail should not be called when not following")
	}
	if out != "hello\nworld\n" {
		t.Errorf("stdout = %q, want hello/world", out)
	}
}

func TestLogsSnapshotLinesFlag(t *testing.T) {
	sess := &fakeSessions{snapshot: "x"}
	_ = captureStdout(t, func() {
		if err := runCmd(t, newLogsCmd(), sess, &fakeWorktrees{}, io.Discard, "sess-1", "-n", "5"); err != nil {
			t.Fatalf("logs: %v", err)
		}
	})
	if sess.snapLines != 5 {
		t.Errorf("snapshot lines = %d, want 5", sess.snapLines)
	}
}

func TestLogsFollow(t *testing.T) {
	sess := &fakeSessions{tailData: "streamed output"}
	out := captureStdout(t, func() {
		if err := runCmd(t, newLogsCmd(), sess, &fakeWorktrees{}, io.Discard, "sess-1", "--follow"); err != nil {
			t.Fatalf("logs -f: %v", err)
		}
	})
	if !sess.tailFollow {
		t.Errorf("Tail follow = false, want true")
	}
	if sess.tailName != "sess-1" {
		t.Errorf("Tail name = %q, want sess-1", sess.tailName)
	}
	if out != "streamed output" {
		t.Errorf("stdout = %q, want streamed output", out)
	}
}

func TestAttachUnsupportedPrintsHint(t *testing.T) {
	sess := &fakeSessions{attachErr: session.ErrAttachUnsupported}
	out := captureStdout(t, func() {
		if err := runCmd(t, newAttachCmd(), sess, &fakeWorktrees{}, io.Discard, "sess-1"); err != nil {
			t.Fatalf("attach should swallow ErrAttachUnsupported, got %v", err)
		}
	})
	if sess.attachName != "sess-1" {
		t.Errorf("attach name = %q, want sess-1", sess.attachName)
	}
	if !strings.Contains(out, "shepherd logs -f sess-1") {
		t.Errorf("hint missing logs -f suggestion, got %q", out)
	}
}

func TestAttachPropagatesOtherErrors(t *testing.T) {
	sess := &fakeSessions{attachErr: domain.NotFoundf("session foo")}
	err := runCmd(t, newAttachCmd(), sess, &fakeWorktrees{}, io.Discard, "foo")
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestStop(t *testing.T) {
	sess := &fakeSessions{}
	var buf strings.Builder
	if err := runCmd(t, newStopCmd(), sess, &fakeWorktrees{}, &buf, "sess-1", "--force"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(sess.stopCalls) != 1 || sess.stopCalls[0].name != "sess-1" || !sess.stopCalls[0].force {
		t.Errorf("stop calls = %+v, want one forced stop of sess-1", sess.stopCalls)
	}
	if !strings.Contains(buf.String(), "Stopped session sess-1") {
		t.Errorf("output = %q, want stopped message", buf.String())
	}
}

func TestStopError(t *testing.T) {
	sess := &fakeSessions{stopErr: domain.NotFoundf("session sess-1")}
	if err := runCmd(t, newStopCmd(), sess, &fakeWorktrees{}, io.Discard, "sess-1"); err == nil {
		t.Fatal("expected stop error to propagate")
	}
}

func TestRm(t *testing.T) {
	wt := domain.Worktree{Name: "crew-1-agent-2", Path: "/tmp/wt", Branch: "feat"}
	sess := &fakeSessions{}
	wts := &fakeWorktrees{get: wt}
	var buf strings.Builder
	if err := runCmd(t, newRmCmd(), sess, wts, &buf, "crew-1-agent-2", "--force"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	// stops the matching session (named after the worktree)
	if len(sess.stopCalls) != 1 || sess.stopCalls[0].name != "crew-1-agent-2" {
		t.Errorf("stop calls = %+v, want stop of crew-1-agent-2", sess.stopCalls)
	}
	// removes the worktree (forced)
	if len(wts.removed) != 1 || wts.removed[0].Path != "/tmp/wt" || !wts.removeForce {
		t.Errorf("worktree removed = %+v force=%v", wts.removed, wts.removeForce)
	}
	// removes the session metadata with purge
	if len(sess.removeCall) != 1 || sess.removeCall[0].name != "crew-1-agent-2" || !sess.removeCall[0].purge {
		t.Errorf("session remove = %+v, want purge of crew-1-agent-2", sess.removeCall)
	}
	if !strings.Contains(buf.String(), "Removed worktree crew-1-agent-2") {
		t.Errorf("output = %q, want removed message", buf.String())
	}
}

func TestRmWorktreeNotFound(t *testing.T) {
	wts := &fakeWorktrees{getErr: domain.NotFoundf("worktree nope")}
	sess := &fakeSessions{}
	if err := runCmd(t, newRmCmd(), sess, wts, io.Discard, "nope"); err == nil {
		t.Fatal("expected not-found error")
	}
	if len(sess.stopCalls) != 0 {
		t.Errorf("should not stop a session when the worktree is missing")
	}
}

func TestRmWorktreeRemoveErrorStops(t *testing.T) {
	wt := domain.Worktree{Name: "wt-1", Path: "/tmp/wt"}
	wts := &fakeWorktrees{get: wt, removeErr: domain.InvalidInputf("dirty")}
	sess := &fakeSessions{}
	if err := runCmd(t, newRmCmd(), sess, wts, io.Discard, "wt-1"); err == nil {
		t.Fatal("expected remove error to propagate")
	}
	// session metadata is NOT purged when the worktree removal fails
	if len(sess.removeCall) != 0 {
		t.Errorf("session metadata should not be removed when worktree removal fails")
	}
}
