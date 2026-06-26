package worktree

import (
	"strings"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	out := "worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\n" +
		"worktree /repo/.wt/feat\nHEAD bbb\nbranch refs/heads/shepherd/feat\n\n"
	wts := parsePorcelain(out)
	if len(wts) != 2 {
		t.Fatalf("want 2 worktrees, got %d", len(wts))
	}
	if !wts[0].IsMain {
		t.Errorf("first worktree should be main")
	}
	if wts[1].IsMain {
		t.Errorf("second worktree should not be main")
	}
	if wts[1].Branch != "shepherd/feat" {
		t.Errorf("branch = %q", wts[1].Branch)
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := sanitizeFilename(`a/b:c*?`)
	if strings.ContainsAny(got, `/\:*?`) {
		t.Errorf("not sanitized: %q", got)
	}
	if sanitizeFilename("CON") == "CON" {
		t.Errorf("reserved device name not handled")
	}
	if sanitizeFilename("") != "task" {
		t.Errorf("empty should fall back to 'task'")
	}
}
