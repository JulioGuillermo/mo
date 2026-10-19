package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SM-01 | Happy    | In mode all a plugin's message goes in unasked, shows in the conversation and wakes an idle model | P0 |
// | SM-02 | Security | In ask mode it waits for the user; let in, it reaches the model          | P0       |
// | SM-03 | Security | Rejected, the model never gets it                                        | P0       |
// | SM-04 | Happy    | "Let in all from" lets that plugin's next messages in unasked, not another's | P1    |
// | SM-05 | Security | After 10 unasked in a row it asks, until the user writes again           | P0       |
// | SM-06 | Happy    | A message sent with wake = false waits for the next turn instead of starting one | P1 |
// | SM-07 | Edge     | Esc puts the question off; /plugins asks it again                        | P2       |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/plugins"
)

func inMode(t *testing.T, mode permissions.Mode) model {
	t.Helper()
	m := conversationModel()
	policy, err := permissions.New(mode, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.session.Agent.Permissions = policy
	return m
}

func sendFromPlugins(m model, messages ...plugins.SentMessage) model {
	for _, message := range messages {
		updated, _ := m.Update(pluginSendMsg(message))
		m = updated.(model)
	}
	return m
}

func choosePopup(t *testing.T, m model, option int) model {
	t.Helper()
	if m.popup == nil || !m.popup.plugin {
		t.Fatalf("no question about a plugin's message:\n%s", shown(m))
	}
	for range option {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(model)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(model)
}

var moved = plugins.SentMessage{Plugin: "jira", Title: "BSSP-1 moved", Text: "It is in review.", Wake: true}

func TestPluginSends(t *testing.T) {
	t.Run("SM-01 in mode all a plugin's message goes in unasked, shows in the conversation and wakes an idle model", func(t *testing.T) {
		m := inMode(t, permissions.ModeAll)

		m = sendFromPlugins(m, moved)

		if m.popup != nil || !m.running || !strings.Contains(shown(m), "⇠ plugin jira · BSSP-1 moved") || !strings.Contains(shown(m), "It is in review.") {
			t.Errorf("popup %v running %v shown:\n%s", m.popup != nil, m.running, shown(m))
		}
	})

	t.Run("SM-02 in ask mode it waits for the user; let in, it reaches the model", func(t *testing.T) {
		m := inMode(t, permissions.ModeAsk)
		m.running = true

		m = sendFromPlugins(m, moved)
		waiting := !m.session.Agent.HasEndedJobs() && strings.Contains(shown(m), "waiting for you")
		m = choosePopup(t, m, 0)

		if !waiting || !m.session.Agent.HasEndedJobs() || !strings.Contains(shown(m), "message from plugin jira let in") {
			t.Errorf("waiting %v shown:\n%s", waiting, shown(m))
		}
	})

	t.Run("SM-03 rejected, the model never gets it", func(t *testing.T) {
		m := inMode(t, permissions.ModeAsk)

		m = sendFromPlugins(m, moved)
		m = choosePopup(t, m, 2)

		if m.session.Agent.HasEndedJobs() || m.running || !strings.Contains(shown(m), "rejected") {
			t.Errorf("running %v shown:\n%s", m.running, shown(m))
		}
	})

	t.Run("SM-04 let in all from lets that plugin's next messages in unasked, not another's", func(t *testing.T) {
		m := inMode(t, permissions.ModeAsk)
		m.running = true

		m = sendFromPlugins(m, moved)
		m = choosePopup(t, m, 1)
		m = sendFromPlugins(m, plugins.SentMessage{Plugin: "jira", Title: "again", Text: "x", Wake: true})
		trustedAsked := m.popup != nil
		m = sendFromPlugins(m, plugins.SentMessage{Plugin: "other", Title: "hi", Text: "y", Wake: true})

		if trustedAsked || m.popup == nil || !strings.Contains(m.popup.title, "plugin other") {
			t.Errorf("trusted asked %v, popup %+v", trustedAsked, m.popup)
		}
	})

	t.Run("SM-05 after 10 unasked in a row it asks, until the user writes again", func(t *testing.T) {
		m := inMode(t, permissions.ModeAll)
		m.running = true

		for range peerStreakMost {
			m = sendFromPlugins(m, moved)
		}
		quiet := m.popup == nil
		m = sendFromPlugins(m, moved)
		asked := m.popup != nil && strings.Contains(m.popup.description, "in a row without you")
		m.popup = nil
		m.pluginSends.pending = nil
		updated, _ := m.submitted("go on")
		m = sendFromPlugins(updated.(model), moved)

		if !quiet || !asked || m.popup != nil {
			t.Errorf("quiet %v asked %v popup after writing %v", quiet, asked, m.popup != nil)
		}
	})

	t.Run("SM-06 a message sent with wake = false waits for the next turn instead of starting one", func(t *testing.T) {
		m := inMode(t, permissions.ModeAll)

		m = sendFromPlugins(m, plugins.SentMessage{Plugin: "jira", Title: "fyi", Text: "later", Wake: false})

		if m.running || m.session.Agent.HasEndedJobs() || !strings.Contains(shown(m), "plugin jira · fyi") {
			t.Errorf("running %v shown:\n%s", m.running, shown(m))
		}
	})

	t.Run("SM-07 esc puts the question off; /plugins asks it again", func(t *testing.T) {
		m := inMode(t, permissions.ModeAsk)
		m.running = true

		m = sendFromPlugins(m, moved)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(model)
		putOff := m.popup == nil && strings.Contains(shown(m), "/plugins answers it")
		updated, _ = m.runCommand("/plugins")
		m = updated.(model)

		if !putOff || m.popup == nil || !m.popup.plugin {
			t.Errorf("put off %v popup %+v", putOff, m.popup)
		}
	})
}
