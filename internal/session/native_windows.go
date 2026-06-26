//go:build windows

package session

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

const stillActive = 259 // STILL_ACTIVE

// configureDetached makes the child its own process group with a hidden console,
// so it survives shepherd exiting, can be tree-killed, and never flashes a
// terminal window. CREATE_NO_WINDOW (rather than DETACHED_PROCESS) gives the
// child a hidden console that its own children inherit, so descendants don't
// flash either.
func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// killTree terminates the process and its descendants (claude spawns node/git/
// ripgrep). taskkill /T walks the tree; /F forces.
func killTree(pid int, force bool) error {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	cmd := exec.Command("taskkill", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.Run()
}
