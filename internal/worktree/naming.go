package worktree

import (
	"strings"

	"github.com/google/uuid"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// names derives the (sanitized) worktree directory name and branch name for a
// task, before any collision suffix is applied.
func names(task domain.Task, nameTemplate, branchPrefix string) (dir, branch string) {
	slug := task.Slug()
	dir = nameTemplate
	if dir == "" {
		dir = "{slug}"
	}
	dir = strings.ReplaceAll(dir, "{slug}", slug)
	dir = strings.ReplaceAll(dir, "{n}", "")
	dir = strings.Trim(dir, "-")
	if dir == "" {
		dir = slug
	}
	dir = sanitizeFilename(dir)
	branch = branchPrefix + dir
	return dir, branch
}

// withSuffix appends a uniqueness suffix to a dir/branch pair.
func withSuffix(dir, branchPrefix, suffix string) (string, string) {
	d := sanitizeFilename(dir + "-" + suffix)
	return d, branchPrefix + d
}

func shortID() string { return uuid.NewString()[:6] }

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// sanitizeFilename makes s safe as a directory name on Windows and Unix:
// strips illegal characters, control chars, trailing dots/spaces, and avoids
// reserved device names.
func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '-'
		}
		if r < 32 {
			return '-'
		}
		return r
	}, s)
	// collapse repeated dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, " .-")
	if s == "" {
		return "task"
	}
	if reservedNames[strings.ToUpper(s)] {
		s += "-wt"
	}
	return s
}
