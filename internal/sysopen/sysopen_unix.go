//go:build !windows

package sysopen

import (
	"io"
	"os/exec"
	"runtime"
)

// Open opens target, a path or an address, with the system's app for it,
// without waiting for the app.
func Open(target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	command := exec.Command(name, target)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait()
	return nil
}
