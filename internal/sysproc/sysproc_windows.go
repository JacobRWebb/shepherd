//go:build windows

package sysproc

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Hide configures cmd to run without a console window (CREATE_NO_WINDOW), so
// background subprocesses (git, gh, claude -p, validation steps, taskkill) never
// flash a terminal. It merges into any existing SysProcAttr.
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
