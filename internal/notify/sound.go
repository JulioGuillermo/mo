package notify

import (
	"context"
	"os/exec"
	"time"
)

// Sound is what a notification sounds like.
type Sound string

const (
	SoundOff    Sound = "off"
	SoundBell   Sound = "bell"
	SoundSystem Sound = "system"
)

// Sounds are the sounds a setting may name.
var Sounds = []Sound{SoundOff, SoundBell, SoundSystem}

const playTimeout = 10 * time.Second

// Chime is how a notification sounds: the bell, sent to the terminal, or a
// sound played here, the file given or else the system's own.
type Chime struct {
	Escape string
	Play   func() error
}

func BuildChime(sound Sound, file string) Chime {
	switch sound {
	case SoundBell:
		return Chime{Escape: "\a"}
	case SoundSystem:
		if play := systemSound(file); play != nil {
			return Chime{Play: play}
		}
		return Chime{Escape: "\a"}
	}
	return Chime{}
}

func runPlayer(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), playTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}
