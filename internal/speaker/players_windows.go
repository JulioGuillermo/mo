package speaker

import (
	"os/exec"
	"strings"
)

// playerCommand is PowerShell playing file with the SoundPlayer Windows
// always has.
func playerCommand(file string) *exec.Cmd {
	quoted := "'" + strings.ReplaceAll(file, "'", "''") + "'"
	return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "(New-Object Media.SoundPlayer "+quoted+").PlaySync()")
}
