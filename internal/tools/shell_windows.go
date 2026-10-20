package tools

import (
	"os"
	"os/exec"
)

func detectShell() Shell {
	return pickWindowsShell(windowsSystem{
		lookPath: exec.LookPath,
		getenv:   os.Getenv,
		exists: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && !info.IsDir()
		},
	})
}
