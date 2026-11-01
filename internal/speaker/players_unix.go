//go:build !windows

package speaker

import (
	"os/exec"
	"runtime"
	"strconv"
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

// livePlayerCommand is a command that plays mono 16-bit PCM at rate from
// its input as it comes, nil when the system has none.
func livePlayerCommand(rate int) *exec.Cmd {
	hertz := strconv.Itoa(rate)
	for _, player := range [][]string{
		{"pw-play", "--raw", "--rate", hertz, "--channels", "1", "--format", "s16", "-"},
		{"paplay", "--raw", "--rate=" + hertz, "--channels=1", "--format=s16le"},
		{"aplay", "-q", "-t", "raw", "-f", "S16_LE", "-r", hertz, "-c", "1"},
	} {
		if path, err := exec.LookPath(player[0]); err == nil {
			return exec.Command(path, player[1:]...)
		}
	}
	return nil
}
