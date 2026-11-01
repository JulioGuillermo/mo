package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/speaker"
)

const voiceCheckTimeout = 10 * time.Second

// voice plays the replies a model says in its own voice, and keeps the last
// to play it again.
type voice struct {
	player speaker.Player
	last   []byte
}

type speechMsg llm.Speech

type voiceChangedMsg struct{}

type voiceCheckedMsg struct {
	model  string
	speaks bool
}

// talker is a provider that can tell whether a model talks.
type talker interface {
	Speaks(ctx context.Context, model string) bool
}

func (m model) voiceCommand(argument string) (tea.Model, tea.Cmd) {
	switch argument {
	case "":
		if m.session.Agent.Speak {
			return m.voiceOff()
		}
		return m, m.checkVoice()
	case "on":
		return m, m.checkVoice()
	case "off":
		return m.voiceOff()
	case "play":
		if m.voice.last == nil {
			return m.emit(m.printer.Note("no reply was spoken yet"))
		}
		return m.play(m.voice.last)
	case "stop":
		m.voice.player.Stop()
		return m, nil
	}
	return m.emit(m.printer.Note("usage: /voice [on|off|play|stop]"))
}

func (m model) voiceOff() (tea.Model, tea.Cmd) {
	m.session.Agent.Speak = false
	m.voice.player.Stop()
	return m.emit(m.printer.Note("voice off: replies are text only"))
}

// checkVoice asks the server whether the model talks, to turn the voice on.
func (m model) checkVoice() tea.Cmd {
	provider, modelName := m.session.Agent.Provider, m.session.Agent.Model
	return func() tea.Msg {
		asked, talks := provider.(talker)
		if !talks {
			return voiceCheckedMsg{model: modelName}
		}
		ctx, cancel := context.WithTimeout(context.Background(), voiceCheckTimeout)
		defer cancel()
		return voiceCheckedMsg{model: modelName, speaks: asked.Speaks(ctx, modelName)}
	}
}

func (m model) voiceChecked(msg voiceCheckedMsg) (tea.Model, tea.Cmd) {
	if !msg.speaks {
		return m.emit(m.printer.Note(fmt.Sprintf("%s does not talk: its server lists no audio among what it writes", shortModelName(msg.model))))
	}
	m.session.Agent.Speak = true
	return m.emit(m.printer.Note("voice on: the model says its replies in its own voice · /voice stop silences one, /voice off turns it off"))
}

func (m model) speech(msg speechMsg) (tea.Model, tea.Cmd) {
	m.voice.last = speaker.WAV(msg.PCM, msg.Rate)
	return m.play(m.voice.last)
}

func (m model) play(wav []byte) (tea.Model, tea.Cmd) {
	if err := m.voice.player.Play(wav); err != nil {
		return m.emitError(err)
	}
	return m, nil
}

// speechLine is the line under a spoken reply.
func (p *printer) speechLine(speech *llm.Speech) string {
	seconds := float64(len(speech.PCM)) / 2 / float64(speech.Rate)
	return p.Note(fmt.Sprintf("♪ said in %.1f s · /voice play to hear it again", seconds))
}

// voiceSegment is the part of the status line that shows the voice is on,
// and plays: a click stops what plays, or plays the last reply again.
func (m model) voiceSegment(colors statusColors) (segment, bool) {
	if !m.session.Agent.Speak {
		return segment{}, false
	}
	text := "♪ voice"
	if m.voice.player.Playing() {
		text = "■ playing"
	}
	toggle := func(m model) (tea.Model, tea.Cmd) {
		if m.voice.player.Playing() {
			return m.voiceCommand("stop")
		}
		return m.voiceCommand("play")
	}
	return segment{text: lipgloss.NewStyle().Foreground(colors.provider).Render(text), bg: colors.segmentDim, do: toggle, menu: toggle}, true
}
