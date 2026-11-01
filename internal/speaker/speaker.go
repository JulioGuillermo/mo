// Package speaker plays speech through the audio player the system has,
// one recording after another.
package speaker

import (
	"encoding/binary"
	"errors"
	"io"
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

// clip is a recording waiting to play: a whole WAV, or speech still coming.
type clip struct {
	wav  []byte
	live *Live
}

// Player plays recordings in the order they come. Changed, when set, hears
// every start and end of one.
type Player struct {
	Changed func()

	mu      sync.Mutex
	queue   []clip
	playing *exec.Cmd
	current clip
}

// Play plays a WAV recording when the ones before it have ended.
func (p *Player) Play(wav []byte) error {
	if playerCommand("") == nil {
		return ErrNoPlayer
	}
	p.enqueue(clip{wav: wav})
	return nil
}

// PlayLive plays speech as it comes, when the ones before it have ended;
// where no player takes it as it comes, it plays once it is all there.
func (p *Player) PlayLive(live *Live) error {
	if playerCommand("") == nil {
		return ErrNoPlayer
	}
	p.enqueue(clip{live: live})
	return nil
}

func (p *Player) enqueue(next clip) {
	p.mu.Lock()
	p.queue = append(p.queue, next)
	idle := p.playing == nil
	p.mu.Unlock()
	if idle {
		p.next()
	}
}

// Stop ends the recording playing and drops the ones waiting.
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, waiting := range p.queue {
		if waiting.live != nil {
			waiting.live.Abandon()
		}
	}
	p.queue = nil
	if p.current.live != nil {
		p.current.live.Abandon()
	}
	if p.playing != nil {
		p.playing.Process.Kill()
	}
}

// Playing tells whether a recording plays.
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
	next := p.queue[0]
	p.queue = p.queue[1:]
	command, cleanup, err := start(next)
	if err != nil {
		p.mu.Unlock()
		p.next()
		return
	}
	p.playing, p.current = command, next
	p.mu.Unlock()
	p.changed()
	go func() {
		command.Wait()
		cleanup()
		p.mu.Lock()
		p.playing, p.current = nil, clip{}
		p.mu.Unlock()
		p.changed()
		p.next()
	}()
}

// start starts the player on a clip: speech still coming goes to a player
// that reads it as it comes, or, where there is none, waits to be whole.
func start(next clip) (*exec.Cmd, func(), error) {
	if next.live != nil {
		if command := livePlayerCommand(next.live.rate); command != nil {
			command.Stdin = next.live
			return command, func() { next.live.Abandon() }, command.Start()
		}
		next.wav = WAV(next.live.whole(), next.live.rate)
	}
	file, err := writeTemp(next.wav)
	if err != nil {
		return nil, nil, err
	}
	command := playerCommand(file)
	if err := command.Start(); err != nil {
		os.Remove(file)
		return nil, nil, err
	}
	return command, func() { os.Remove(file) }, nil
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

// Live is speech that comes in pieces: what is written to it is read in
// order, the reads waiting for more until it is closed.
type Live struct {
	rate      int
	mu        sync.Mutex
	more      *sync.Cond
	pcm       []byte
	read      int
	closed    bool
	abandoned bool
}

// NewLive is speech to come at rate samples a second.
func NewLive(rate int) *Live {
	live := &Live{rate: rate}
	live.more = sync.NewCond(&live.mu)
	return live
}

func (l *Live) Write(pcm []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pcm = append(l.pcm, pcm...)
	l.more.Broadcast()
	return len(pcm), nil
}

// Close says all of it came.
func (l *Live) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	l.more.Broadcast()
	return nil
}

// Abandon ends the reads now, with what is left unread dropped.
func (l *Live) Abandon() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed, l.abandoned = true, true
	l.more.Broadcast()
}

func (l *Live) Read(into []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for l.read == len(l.pcm) && !l.closed {
		l.more.Wait()
	}
	if l.abandoned || l.read == len(l.pcm) {
		return 0, io.EOF
	}
	n := copy(into, l.pcm[l.read:])
	l.read += n
	return n, nil
}

// whole waits for all of it and returns it.
func (l *Live) whole() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	for !l.closed {
		l.more.Wait()
	}
	if l.abandoned {
		return nil
	}
	return l.pcm
}
