//go:build !windows

// Package sysproc adjusts how child processes are spawned per-OS. On Windows it
// hides the console window so background subprocesses don't flash a terminal; on
// other platforms the helpers are no-ops.
package sysproc

import "os/exec"

// Hide configures cmd to run without spawning a visible console window. On
// non-Windows platforms this is a no-op.
func Hide(cmd *exec.Cmd) {}
