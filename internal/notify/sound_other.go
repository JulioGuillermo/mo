//go:build !windows

package notify

import (
	"os"
	"runtime"
)

const (
	freedesktopSound = "/usr/share/sounds/freedesktop/stereo/message-new-instant.oga"
	macSound         = "/System/Library/Sounds/Glass.aiff"
)

// systemSound plays the file, or the desktop's sound for a new message,
// with the first player found; nil when there is none.
func systemSound(file string) func() error {
	if runtime.GOOS == "darwin" {
		return func() error { return runPlayer("afplay", orDefault(file, macSound)) }
	}
	if file == "" && found("canberra-gtk-play") {
		return func() error { return runPlayer("canberra-gtk-play", "-i", "message-new-instant") }
	}
	sound := orDefault(file, freedesktopSound)
	if _, err := os.Stat(sound); err != nil {
		return nil
	}
	for _, player := range []string{"pw-play", "paplay", "canberra-gtk-play"} {
		if !found(player) {
			continue
		}
		if player == "canberra-gtk-play" {
			return func() error { return runPlayer(player, "-f", sound) }
		}
		return func() error { return runPlayer(player, sound) }
	}
	return nil
}

func orDefault(file, fallback string) string {
	if file == "" {
		return fallback
	}
	return file
}
