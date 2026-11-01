//go:build !windows

package speaker

import (
	"os/exec"
	"runtime"
)

// players are the commands that play a WAV file, the first found used.
var players = [][]string{
	{"pw-play"},
	{"paplay"},
	{"aplay", "-q"},
	{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"},
}

// playerCommand is the command that plays file, nil when the system has no
// player.
func playerCommand(file string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("afplay", file)
	}
	for _, player := range players {
		if path, err := exec.LookPath(player[0]); err == nil {
			return exec.Command(path, append(player[1:], file)...)
		}
	}
	return nil
}
