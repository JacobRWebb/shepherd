package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	got := sanitizeName("crew/1:2*x")
	for _, r := range got {
		if r == '/' || r == ':' || r == '*' {
			t.Errorf("not sanitized: %q", got)
		}
	}
}

func TestLastLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(p, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := lastLines(p, 2); got != "c\nd" {
		t.Errorf("lastLines = %q", got)
	}
	if got, _ := lastLines(filepath.Join(t.TempDir(), "missing"), 2); got != "" {
		t.Errorf("missing file should be empty, got %q", got)
	}
}

func TestReadExitSentinel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	_ = os.WriteFile(p, []byte("some output\n[[SHEP_EXIT:3]]\n"), 0o644)
	code, ok := readExitSentinel(p)
	if !ok || code != 3 {
		t.Errorf("code=%d ok=%v", code, ok)
	}
	_ = os.WriteFile(p, []byte("no sentinel here"), 0o644)
	if _, ok := readExitSentinel(p); ok {
		t.Errorf("should not find a sentinel")
	}
}
