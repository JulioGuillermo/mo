// Package speaker plays speech through the audio player the system has,
// one recording after another.
package speaker

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"sync"
)

// ErrNoPlayer is returned where the system has none of the players known.
var ErrNoPlayer = errors.New("no audio player found: install pw-play, paplay, aplay or ffplay")

// WAV wraps mono 16-bit PCM in a WAV header.
func WAV(pcm []byte, rate int) []byte {
	header := []byte("RIFF")
	header = binary.LittleEndian.AppendUint32(header, uint32(36+len(pcm)))
	header = append(header, "WAVEfmt "...)
	header = binary.LittleEndian.AppendUint32(header, 16)
	header = binary.LittleEndian.AppendUint16(header, 1)
	header = binary.LittleEndian.AppendUint16(header, 1)
	header = binary.LittleEndian.AppendUint32(header, uint32(rate))
	header = binary.LittleEndian.AppendUint32(header, uint32(rate*2))
	header = binary.LittleEndian.AppendUint16(header, 2)
	header = binary.LittleEndian.AppendUint16(header, 16)
	header = append(header, "data"...)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(pcm)))
	return append(header, pcm...)
}

// Player plays WAV recordings in the order they come. Changed, when set,
// hears every start and end of one.
type Player struct {
	Changed func()

	mu      sync.Mutex
	queue   [][]byte
	playing *exec.Cmd
}

// Play plays the recording when the ones before it have ended.
func (p *Player) Play(wav []byte) error {
	if playerCommand("") == nil {
		return ErrNoPlayer
	}
	p.mu.Lock()
	p.queue = append(p.queue, wav)
	idle := p.playing == nil
	p.mu.Unlock()
	if idle {
		p.next()
	}
	return nil
}

// Stop ends the recording playing and drops the ones waiting.
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = nil
	if p.playing != nil {
		p.playing.Process.Kill()
	}
}

// Playing tells whether a recording is playing.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing != nil
}

func (p *Player) next() {
	p.mu.Lock()
	if len(p.queue) == 0 || p.playing != nil {
		p.mu.Unlock()
		return
	}
	wav := p.queue[0]
	p.queue = p.queue[1:]
	file, err := writeTemp(wav)
	var command *exec.Cmd
	if err == nil {
		command = playerCommand(file)
		err = command.Start()
	}
	if err != nil {
		os.Remove(file)
		p.mu.Unlock()
		p.next()
		return
	}
	p.playing = command
	p.mu.Unlock()
	p.changed()
	go func() {
		command.Wait()
		os.Remove(file)
		p.mu.Lock()
		p.playing = nil
		p.mu.Unlock()
		p.changed()
		p.next()
	}()
}

func (p *Player) changed() {
	if p.Changed != nil {
		p.Changed()
	}
}

func writeTemp(wav []byte) (string, error) {
	file, err := os.CreateTemp("", "mo-speech-*.wav")
	if err != nil {
		return "", err
	}
	_, err = file.Write(wav)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return file.Name(), err
}
