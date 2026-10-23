package notify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

const soundScript = `(New-Object Media.SoundPlayer $env:MO_SOUND).PlaySync()`

// systemSound plays a WAV file, by default Windows' own notification
// sound, through Windows PowerShell.
func systemSound(file string) func() error {
	systemRoot := os.Getenv("SystemRoot")
	powershell := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(powershell); err != nil {
		return nil
	}
	if file == "" {
		file = filepath.Join(systemRoot, "Media", "Windows Notify System Generic.wav")
	}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), playTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", soundScript)
		command.Env = append(os.Environ(), "MO_SOUND="+file)
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		return command.Run()
	}
}
