// Package domain defines Shepherd's core entities and sentinel errors.
//
// It is the innermost layer of the application: it imports nothing else in the
// project. Every other package (worktree, agent, session, forge, pipeline, ...)
// maps its own provider- or tool-specific data into these neutral types, so no
// gh/Bitbucket/tmux detail ever leaks outward.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors. Wrap these with fmt.Errorf("%w: ...", ErrX) and compare with
// errors.Is. The CLI maps each to a distinct process exit code.
var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("conflict")
	ErrNotGitRepo   = errors.New("not a git repository")
	ErrDirty        = errors.New("working tree has uncommitted changes")
	ErrUnsupported  = errors.New("unsupported operation")
)

// InvalidInputf builds an error that satisfies errors.Is(err, ErrInvalidInput)
// while carrying a specific, human-readable message.
func InvalidInputf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// NotFoundf builds an error that satisfies errors.Is(err, ErrNotFound).
func NotFoundf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotFound, fmt.Sprintf(format, args...))
}

// Conflictf builds an error that satisfies errors.Is(err, ErrConflict).
func Conflictf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, args...))
}
