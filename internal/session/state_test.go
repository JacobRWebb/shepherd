package session

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundtrip(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Info{Name: "a", Backend: BackendNative, State: StateRunning, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a")
	if err != nil || got.State != StateRunning {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	if all, _ := s.All(); len(all) != 1 {
		t.Errorf("all = %d", len(all))
	}
	if err := s.Patch("a", func(i *Info) { i.State = StateStopped }); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("a"); got.State != StateStopped {
		t.Errorf("patch failed: %v", got.State)
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a"); err == nil {
		t.Errorf("expected ErrNotFound after delete")
	}
}

func TestStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s1, _ := OpenStore(path)
	_ = s1.Upsert(Info{Name: "x", Backend: BackendTmux, State: StateRunning, StartedAt: time.Now()})
	// a fresh store reading the same file should see it
	s2, _ := OpenStore(path)
	if got, err := s2.Get("x"); err != nil || got.Backend != BackendTmux {
		t.Errorf("persistence failed: %+v err=%v", got, err)
	}
}
