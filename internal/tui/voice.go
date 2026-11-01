package tui

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
	"github.com/jg/mo/internal/speaker"
)

const voiceCheckTimeout = 10 * time.Second

// voice is the model's voice in this session: on or off, the replies it
// said that are not saved, and the one it says now, played as it comes.
type voice struct {
	on     bool
	player speaker.Player
	// unsaved are the replies said that were not saved, by their key.
	unsaved map[string][]byte
	last    string
	live    *speaker.Live
	// liveQueued says live was handed to the player.
	liveQueued bool
}

// speechPieceMsg is a piece of the reply said; afterText says the reply
// has written text before it.
type speechPieceMsg struct {
	speech    llm.Speech
	afterText bool
}

// replyEndedMsg ends a reply: what it said, nil for nothing, and the file
// it was saved to.
type replyEndedMsg struct {
	speech *llm.Speech
	file   string
}

type voiceChangedMsg struct{}

type voiceCheckedMsg struct {
	model  string
	speaks bool
}

// talker is a provider that can tell whether a model talks.
type talker interface {
	Speaks(ctx context.Context, model string) bool
	EndVoice()
}

// voiceOn tells whether the model's voice is on; a model built without the
// voice, as in tests, has it off.
func (m model) voiceOn() bool {
	return m.voice != nil && m.voice.on
}

func (m model) voiceCommand(argument string) (tea.Model, tea.Cmd) {
	if m.voice == nil {
		m.voice = &voice{}
	}
	switch argument {
	case "":
		if m.voice.on {
			return m.voiceOff()
		}
		return m, m.checkVoice()
	case "on":
		return m, m.checkVoice()
	case "off":
		return m.voiceOff()
	case "play":
		if m.voice.last == "" {
			return m.emit(m.printer.Note("no reply was said yet"))
		}
		return m.playSpeech(m.voice.last)
	case "stop":
		m.voice.player.Stop()
		return m, nil
	}
	return m.emit(m.printer.Note("usage: /voice [on|off|play|stop] · /settings voice for how it speaks"))
}

func (m model) voiceOff() (tea.Model, tea.Cmd) {
	m.voice.on = false
	m.voice.player.Stop()
	endVoice(m.session.Agent.Provider)
	return m.emit(m.printer.Note("voice off: replies are text only"))
}

// endVoice closes the provider's realtime conversation, which holds a slot
// of the server while open.
func endVoice(provider llm.Provider) {
	if talking, ok := provider.(talker); ok {
		talking.EndVoice()
	}
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
	m.voice.on = true
	note := fmt.Sprintf("voice on, %s: click a ♪ line to hear it again · /voice off turns it off · /settings voice", m.session.Config.Voice.Mode)
	return m.emit(m.printer.Note(note))
}

// speakWith sets the agent to speak, or not, as the voice and its settings
// say, before a run.
func (m model) speakWith(agentVoice *string, speakCalls *bool) {
	*agentVoice, *speakCalls = "", false
	if m.voiceOn() {
		settings := m.session.Config.Voice
		*agentVoice, *speakCalls = settings.Mode, *settings.SpeakCalls
	}
}

// speechSaver saves a reply's speech in the conversation's folder when the
// settings keep it.
func (m model) speechSaver() func(llm.Speech) string {
	if !m.voiceOn() || !*m.session.Config.Voice.Save {
		return nil
	}
	dir := sessions.SpeechDir(config.SessionDir(), m.thread.id)
	return func(speech llm.Speech) string {
		name := time.Now().Format("20060102-150405.000") + ".wav"
		if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(filepath.Join(dir, name), speaker.WAV(speech.PCM, speech.Rate), 0o600) != nil {
			return ""
		}
		return name
	}
}

// speechPiece plays the reply as it is said, when the settings ask: once
// it has written text, as a reply that only calls tools writes none, or at
// once where replies are not written or theirs are said too.
func (m model) speechPiece(msg speechPieceMsg) (tea.Model, tea.Cmd) {
	settings := m.session.Config.Voice
	if !m.voiceOn() || !*settings.Autoplay || !*settings.Live {
		return m, nil
	}
	if m.voice.live == nil {
		m.voice.live = speaker.NewLive(msg.speech.Rate)
	}
	m.voice.live.Write(msg.speech.PCM)
	if !m.voice.liveQueued && (msg.afterText || *settings.SpeakCalls || settings.Mode != config.VoiceTextAudio) {
		m.voice.liveQueued = true
		if err := m.voice.player.PlayLive(m.voice.live); err != nil {
			return m.emitError(err)
		}
	}
	return m, nil
}

// replyEnded shows the line of the reply said, and plays it when it did not
// play as it came; a reply said in part only, as one that only called
// tools, is dropped.
func (m model) replyEnded(msg replyEndedMsg) (tea.Model, tea.Cmd) {
	if m.voice == nil {
		return m, nil
	}
	live, queued := m.voice.live, m.voice.liveQueued
	m.voice.live, m.voice.liveQueued = nil, false
	if msg.speech == nil {
		if live != nil {
			live.Abandon()
		}
		return m, nil
	}
	wav := speaker.WAV(msg.speech.PCM, msg.speech.Rate)
	key := m.speechPath(msg.file)
	if msg.file == "" {
		key = fmt.Sprintf("unsaved %d", len(m.voice.unsaved))
		m.voice.unsaved = withEntry(m.voice.unsaved, key, wav)
	}
	m.voice.last = key
	m.addSpeechLine(key, speechSeconds(len(msg.speech.PCM), msg.speech.Rate))
	switch {
	case live != nil && queued:
		live.Close()
	case live != nil:
		live.Abandon()
		fallthrough
	case *m.session.Config.Voice.Autoplay:
		if err := m.voice.player.Play(wav); err != nil {
			return m.emitError(err)
		}
	}
	return m, nil
}

// silenceUnfinished drops the reply said in part when a run ends without
// ending it, as when it is stopped.
func (m *model) silenceUnfinished() {
	if m.voice != nil && m.voice.live != nil {
		m.voice.live.Abandon()
		m.voice.live, m.voice.liveQueued = nil, false
	}
}

// speechPath is where a reply's speech saved in this conversation is.
func (m model) speechPath(file string) string {
	if file == "" {
		return ""
	}
	id := m.thread.id
	if id == "" {
		id = m.thread.parent
	}
	return filepath.Join(sessions.SpeechDir(config.SessionDir(), id), file)
}

func (m *model) addSpeechLine(key string, seconds float64) {
	printer := m.printer
	draw := func(bool) string {
		return "\n" + printer.Note(fmt.Sprintf("♪ %.1f s · click to hear it, or to stop what plays", seconds))
	}
	m.blocks = append(m.blocks, block{text: draw(false), draw: draw, speech: key})
}

// playSpeech plays a reply said, from memory or its file; when something
// plays, it stops it instead.
func (m model) playSpeech(key string) (tea.Model, tea.Cmd) {
	if m.voice == nil {
		m.voice = &voice{}
	}
	if m.voice.player.Playing() {
		m.voice.player.Stop()
		return m, nil
	}
	wav, kept := m.voice.unsaved[key]
	if !kept {
		read, err := os.ReadFile(key)
		if err != nil {
			return m.emitError(fmt.Errorf("the reply said is gone: %w", err))
		}
		wav = read
	}
	if err := m.voice.player.Play(wav); err != nil {
		return m.emitError(err)
	}
	return m, nil
}

// speechAt is the reply said shown at row y of the conversation.
func (m model) speechAt(y int) (string, bool) {
	conversation := m.conversation(m.conversationHeight())
	i, found := m.conversationLines().blockAt(y + conversation.YOffset())
	if !found || m.blocks[i].speech == "" {
		return "", false
	}
	return m.blocks[i].speech, true
}

func speechSeconds(bytes, rate int) float64 {
	if rate <= 0 {
		return 0
	}
	return float64(bytes) / 2 / float64(rate)
}

// savedSpeechSeconds is how long a saved reply said lasts, read from its
// WAV header; zero when the file is gone.
func savedSpeechSeconds(path string) (float64, bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()
	header := make([]byte, 44)
	if _, err := io.ReadFull(file, header); err != nil {
		return 0, false
	}
	rate, size := binary.LittleEndian.Uint32(header[24:]), binary.LittleEndian.Uint32(header[40:])
	return speechSeconds(int(size), int(rate)), true
}

// voiceSegment is the part of the status line that shows the voice is on,
// and plays: a click stops what plays, or plays the last reply again.
func (m model) voiceSegment(colors statusColors) (segment, bool) {
	if !m.voiceOn() {
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
