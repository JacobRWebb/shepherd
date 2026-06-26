//go:build !windows

package session

import (
	"os/exec"
	"syscall"
)

// configureDetached puts the child in its own process group (pgid == pid) so it
// survives shepherd exiting and the whole group can be signalled.
func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// killTree signals the whole process group (negative pid). SIGTERM unless force.
func killTree(pid int, force bool) error {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-pid, sig); err != nil {
		return syscall.Kill(pid, sig)
	}
	return nil
}
