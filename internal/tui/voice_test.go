package tui

// Test Traceability Table
// | ID    | Category | Description                                                            | Priority |
// | VO-01 | Happy    | /voice turns the voice on for a model that talks, and off again        | P0       |
// | VO-02 | Edge     | /voice for a model that does not talk says so and leaves it off        | P0       |
// | VO-03 | Edge     | /voice play before any spoken reply says there is none                 | P2       |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
)

type talkingProvider struct {
	scriptedReplies
	talks bool
}

func (p talkingProvider) Speaks(context.Context, string) bool {
	return p.talks
}

func voiceModel(talks bool) model {
	m := conversationModel()
	m.session.Agent = &agent.Agent{Provider: talkingProvider{talks: talks}, Model: "omni"}
	m.voice = &voice{}
	return m
}

func TestVoiceCommand(t *testing.T) {
	t.Run("VO-01 /voice turns the voice on for a model that talks, and off again", func(t *testing.T) {
		m := voiceModel(true)

		updated, cmd := m.runCommand("/voice")
		m = run(t, updated.(model), cmd)
		on := m.session.Agent.Speak
		updated, _ = m.runCommand("/voice")
		m = updated.(model)

		if !on || m.session.Agent.Speak || !strings.Contains(shown(m), "voice on") || !strings.Contains(shown(m), "voice off") {
			t.Errorf("on %v, then %v; transcript:\n%s", on, m.session.Agent.Speak, shown(m))
		}
	})

	t.Run("VO-02 /voice for a model that does not talk says so and leaves it off", func(t *testing.T) {
		m := voiceModel(false)

		updated, cmd := m.runCommand("/voice on")
		m = run(t, updated.(model), cmd)

		if m.session.Agent.Speak || !strings.Contains(shown(m), "omni does not talk") {
			t.Errorf("speak %v; transcript:\n%s", m.session.Agent.Speak, shown(m))
		}
	})

	t.Run("VO-03 /voice play before any spoken reply says there is none", func(t *testing.T) {
		updated, _ := voiceModel(true).runCommand("/voice play")

		if !strings.Contains(shown(updated.(model)), "no reply was spoken yet") {
			t.Errorf("transcript:\n%s", shown(updated.(model)))
		}
	})
}
