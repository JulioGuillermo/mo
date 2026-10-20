// Package proctree runs a command apart from mo's terminal and stops it
// together with everything it started.
package proctree

import "os/exec"

// KillOnCancel makes a cancelled context kill the command's whole tree.
// The command must have been set apart with Detach.
func KillOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return Kill(cmd.Process.Pid) }
}
