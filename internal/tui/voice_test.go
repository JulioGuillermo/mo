package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | VO-01 | Happy    | /voice turns the voice on for a model that talks, and off again              | P0       |
// | VO-02 | Edge     | /voice for a model that does not talk says so and leaves it off              | P0       |
// | VO-03 | Edge     | /voice play before any reply said says there is none                         | P2       |
// | VO-04 | Happy    | A run speaks as the voice settings say, and saves into the conversation's folder | P0   |
// | VO-05 | Happy    | A reply said shows a ♪ line that plays it on a click, saved or kept in memory | P0       |
// | VO-06 | Happy    | Autoplay off leaves a reply said silent until its line is clicked            | P1       |
// | VO-07 | Happy    | A resumed conversation shows a ♪ line for each reply said still on disk      | P0       |
// | VO-08 | Happy    | Played as it comes: once the reply writes text, and dropped when it only called tools | P0 |

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

type talkingProvider struct {
	scriptedReplies
	talks bool
}

func (p talkingProvider) Speaks(context.Context, string) bool {
	return p.talks
}

func (talkingProvider) EndVoice() {}

func voiceModel(talks bool) model {
	m := conversationModel()
	m.session.Agent = &agent.Agent{Provider: talkingProvider{talks: talks}, Model: "omni"}
	m.voice = &voice{}
	return m
}

// fakeSpeaker is a pw-play that logs what it plays: the last bytes of a
// file, or all it reads when its last argument is -.
func fakeSpeaker(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake player is a shell script")
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "played")
	script := "#!/bin/sh\nfor last; do :; done\nif [ \"$last\" = - ]; then cat >> " + log + "; else tail -c 2 \"$last\" >> " + log + "; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "pw-play"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return log
}

func played(t *testing.T, m model, log string) string {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	deadline := time.Now().Add(3 * time.Second)
	for m.voice.player.Playing() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(log)
	return string(data)
}

func lineOfSpeech(m model) int {
	for i, line := range strings.Split(shown(m), "\n") {
		if strings.Contains(line, "♪") {
			return i
		}
	}
	return -1
}

func TestVoiceCommand(t *testing.T) {
	t.Run("VO-01 /voice turns the voice on for a model that talks, and off again", func(t *testing.T) {
		m := voiceModel(true)

		updated, cmd := m.runCommand("/voice")
		m = run(t, updated.(model), cmd)
		on := m.voice.on
		updated, _ = m.runCommand("/voice")
		m = updated.(model)

		if !on || m.voice.on || !strings.Contains(shown(m), "voice on, text+audio") || !strings.Contains(shown(m), "voice off") {
			t.Errorf("on %v, then %v; transcript:\n%s", on, m.voice.on, shown(m))
		}
	})

	t.Run("VO-02 /voice for a model that does not talk says so and leaves it off", func(t *testing.T) {
		m := voiceModel(false)

		updated, cmd := m.runCommand("/voice on")
		m = run(t, updated.(model), cmd)

		if m.voice.on || !strings.Contains(shown(m), "omni does not talk") {
			t.Errorf("on %v; transcript:\n%s", m.voice.on, shown(m))
		}
	})

	t.Run("VO-03 /voice play before any reply said says there is none", func(t *testing.T) {
		updated, _ := voiceModel(true).runCommand("/voice play")

		if !strings.Contains(shown(updated.(model)), "no reply was said yet") {
			t.Errorf("transcript:\n%s", shown(updated.(model)))
		}
	})
}

func TestVoicePlayback(t *testing.T) {
	speech := llm.Speech{PCM: []byte("x1"), Rate: 24000}

	t.Run("VO-04 a run speaks as the voice settings say, and saves into the conversation's folder", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		m := voiceModel(true)
		m.session.Config.Voice.Mode, m.session.Config.Voice.SpeakCalls = config.VoiceAudio, ptr(true)
		offMode, offCalls := "stale", true
		m.speakWith(&offMode, &offCalls)
		m.voice.on = true
		mode, speakCalls := "", false
		m.speakWith(&mode, &speakCalls)
		m.thread.id = "conv1"

		name := m.speechSaver()(speech)
		m.session.Config.Voice.Save = ptr(false)

		if offMode != "" || offCalls || mode != config.VoiceAudio || !speakCalls {
			t.Errorf("off: %q %v; on: %q %v", offMode, offCalls, mode, speakCalls)
		}
		data, err := os.ReadFile(filepath.Join(sessions.SpeechDir(config.SessionDir(), "conv1"), name))
		if err != nil || string(data[:4]) != "RIFF" || m.speechSaver() != nil {
			t.Errorf("saved %q: %v", name, err)
		}
	})

	t.Run("VO-05 a reply said shows a ♪ line that plays it on a click, saved or kept in memory", func(t *testing.T) {
		log := fakeSpeaker(t)
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		m := voiceModel(true)
		m.session.Config.Voice.Autoplay = ptr(false)
		m.thread.id = "conv1"
		m.voice.on = true
		saved := m.speechSaver()(llm.Speech{PCM: []byte("s1"), Rate: 24000})

		updated, _ := m.replyEnded(replyEndedMsg{speech: &llm.Speech{PCM: []byte("s1"), Rate: 24000}, file: saved})
		updated, _ = updated.(model).replyEnded(replyEndedMsg{speech: &speech})
		m = updated.(model)
		lines := strings.Split(shown(m), "\n")
		var clicked []string
		for i, line := range lines {
			if strings.Contains(line, "♪") {
				key, found := m.speechAt(i - m.conversationOffset())
				if !found {
					t.Fatalf("line %d %q is not a reply said", i, line)
				}
				m.playSpeech(key)
				clicked = append(clicked, played(t, m, log))
			}
		}

		if len(clicked) != 2 || clicked[0] != "s1" || clicked[1] != "s1x1" {
			t.Errorf("played %q; transcript:\n%s", clicked, shown(m))
		}
	})

	t.Run("VO-06 autoplay off leaves a reply said silent until its line is clicked", func(t *testing.T) {
		log := fakeSpeaker(t)
		m := voiceModel(true)
		m.session.Config.Voice.Autoplay = ptr(false)

		updated, _ := m.replyEnded(replyEndedMsg{speech: &speech})
		m = updated.(model)

		if got := played(t, m, log); got != "" || lineOfSpeech(m) < 0 {
			t.Errorf("played %q; transcript:\n%s", got, shown(m))
		}
	})

	t.Run("VO-07 a resumed conversation shows a ♪ line for each reply said still on disk", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		m := voiceModel(true)
		m.voice.on = true
		m.thread.id = "conv1"
		name := m.speechSaver()(llm.Speech{PCM: make([]byte, 48000), Rate: 24000})
		history := []llm.Message{
			{Role: llm.RoleUser, Text: "hi"},
			{Role: llm.RoleAssistant, Text: "hello", SpeechFile: name},
			{Role: llm.RoleAssistant, Text: "gone", SpeechFile: "missing.wav"},
		}

		m.replay(history)

		if text := shown(m); strings.Count(text, "♪") != 1 || !strings.Contains(text, "♪ 1.0 s") {
			t.Errorf("transcript:\n%s", text)
		}
	})

	t.Run("VO-08 played as it comes: once the reply writes text, and dropped when it only called tools", func(t *testing.T) {
		log := fakeSpeaker(t)
		m := voiceModel(true)
		m.voice.on = true
		m.session.Config.Voice.Live = ptr(true)

		updated, _ := m.speechPiece(speechPieceMsg{speech: llm.Speech{PCM: []byte("t1"), Rate: 24000}})
		updated, _ = updated.(model).replyEnded(replyEndedMsg{})
		m = updated.(model)
		dropped := played(t, m, log)
		updated, _ = m.speechPiece(speechPieceMsg{speech: llm.Speech{PCM: []byte("a1"), Rate: 24000}, afterText: true})
		m = updated.(model)
		updated, _ = m.speechPiece(speechPieceMsg{speech: llm.Speech{PCM: []byte("b2"), Rate: 24000}, afterText: true})
		updated, _ = updated.(model).replyEnded(replyEndedMsg{speech: &llm.Speech{PCM: []byte("a1b2"), Rate: 24000}})
		m = updated.(model)

		if dropped != "" || played(t, m, log) != "a1b2" || lineOfSpeech(m) < 0 {
			t.Errorf("dropped %q, played %q", dropped, played(t, m, log))
		}
	})
}

func ptr[T any](value T) *T {
	return &value
}
