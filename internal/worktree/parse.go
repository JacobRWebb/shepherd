package worktree

import (
	"path/filepath"
	"strings"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// parsePorcelain parses `git worktree list --porcelain` output. Records are
// separated by blank lines; the first record is always the main work tree.
func parsePorcelain(out string) []domain.Worktree {
	var wts []domain.Worktree
	var cur *domain.Worktree
	flush := func() {
		if cur != nil {
			wts = append(wts, *cur)
			cur = nil
		}
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			p := filepath.Clean(strings.TrimPrefix(line, "worktree "))
			cur = &domain.Worktree{Path: p, Name: filepath.Base(p)}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
		}
	}
	flush()
	if len(wts) > 0 {
		wts[0].IsMain = true // git lists the main work tree first
	}
	return wts
}
