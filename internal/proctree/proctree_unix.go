//go:build !windows

package proctree

import (
	"os/exec"
	"syscall"
)

// Detach runs the command in a session of its own, without a controlling
// terminal: a password prompt fails instead of waiting, and the session's
// process group holds everything the command starts.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// Terminate asks the tree started by pid to end.
func Terminate(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// Kill ends the tree started by pid at once.
func Kill(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// Alive tells whether anything in the tree started by pid still runs.
func Alive(pid int) bool {
	return syscall.Kill(-pid, 0) == nil
}
