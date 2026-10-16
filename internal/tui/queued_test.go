package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | QM-01 | Happy    | A queued message stays in view over the status line, however long the reply | P0    |
// | QM-02 | Happy    | Read by the model, it goes into the conversation after what came before and leaves the line | P0 |
// | QM-03 | Happy    | Stopped, what was queued is back in the input and no longer waits        | P1       |
// | QM-04 | Edge     | Settings saved during the turn keep what was queued                      | P1       |

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

func queuedModel(t *testing.T) model {
	t.Helper()
	m := conversationModel()
	m.running = true
	m.cancel = func() {}
	m = streaming(m, llm.Event{Kind: llm.EventText, Text: strings.Repeat("a long reply line\n", 100)})
	m.input.SetValue("please also do X")
	return enter(t, m)
}

func TestQueuedMessages(t *testing.T) {
	t.Run("QM-01 a queued message stays in view over the status line, however long the reply", func(t *testing.T) {
		m := queuedModel(t)

		view := ansi.Strip(m.View().Content)

		if !strings.Contains(view, "queued › please also do X") || strings.Contains(shown(m), "please also do X") {
			t.Errorf("view:\n%s\ntranscript:\n%s", view, shown(m))
		}
	})

	t.Run("QM-02 read by the model, it goes into the conversation after what came before and leaves the line", func(t *testing.T) {
		m := queuedModel(t)
		updated, _ := m.Update(streamEndMsg{})
		m = updated.(model)
		m = deliver(m, func() tea.Msg { return drawMsg(func() string { return m.printer.Answer("the reply") }) }, 2)

		updated, cmd := m.Update(queuedReadMsg(1))
		m = deliver(updated.(model), cmd, 3)

		transcript := shown(m)
		reply, asked := strings.Index(transcript, "the reply"), strings.Index(transcript, "please also do X")
		if reply < 0 || asked < reply || strings.Contains(ansi.Strip(m.View().Content), "queued ›") {
			t.Errorf("transcript:\n%s", transcript)
		}
	})

	t.Run("QM-03 stopped, what was queued is back in the input and no longer waits", func(t *testing.T) {
		m := queuedModel(t)

		updated, _ := m.finishRun(context.Canceled)
		m = updated.(model)

		if m.input.Value() != "please also do X" || len(m.queued) != 0 {
			t.Errorf("input %q queued %v", m.input.Value(), m.queued)
		}
	})

	t.Run("QM-04 settings saved during the turn keep what was queued", func(t *testing.T) {
		m := queuedModel(t)
		fresh := m.session
		fresh.Agent = &agent.Agent{}

		takeOver(fresh, m.session)

		if !fresh.Agent.HasQueued() || m.session.Agent.HasQueued() {
			t.Error("the queued message did not move to the new session")
		}
	})
}
