package proctree

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// Detach runs the command in a process group of its own and without a
// console, so a prompt for input fails instead of waiting.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW}
}

// Terminate ends the tree started by pid; console programs on Windows
// cannot be asked to end, so it is the same as Kill.
func Terminate(pid int) error {
	return Kill(pid)
}

// Kill ends the tree started by pid at once.
func Kill(pid int) error {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

// Alive tells whether the process pid still runs.
func Alive(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	event, err := windows.WaitForSingleObject(process, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}
