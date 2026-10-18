package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | QM-01 | Happy    | A queued message stays in view over the status line, however long the reply | P0    |
// | QM-02 | Happy    | Read by the model, it goes into the conversation after what came before and leaves the line | P0 |
// | QM-03 | Happy    | Stopped, what was queued is back in the input and no longer waits        | P1       |
// | QM-04 | Edge     | Settings saved during the turn keep what was queued                      | P1       |
// | QM-05 | Happy    | Several queued show as one entry over the status line                    | P0       |
// | QM-06 | Happy    | Read by the model, several go into the conversation as one message       | P0       |
// | QM-07 | Happy    | Up with nothing typed brings all that is queued back to the input        | P0       |
// | QM-08 | Edge     | Up with something typed, or with nothing queued, goes through the history | P0      |
// | QM-09 | Happy    | ctrl+x clears what is typed while the model works, and stops nothing     | P0       |
// | QM-10 | Edge     | A long queued message wraps inside the main panel, never under the sidebar | P0     |
// | QM-12 | Happy    | The queue is shaded, in a colour neither the user's panel nor the chat has | P1      |
// | QM-13 | Happy    | The mark of a message follows the icon set, and an amber edge runs down the queue | P1 |
// | QM-11 | Edge     | A queued message of many lines is cut with a count of those not shown    | P1       |

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
	m.session.Config.Render.Icons = iconsEmoji
	m = streaming(m, llm.Event{Kind: llm.EventText, Text: strings.Repeat("a long reply line\n", 100)})
	m.input.SetValue("please also do X")
	return enter(t, m)
}

func TestQueuedMessages(t *testing.T) {
	t.Run("QM-01 a queued message stays in view over the status line, however long the reply", func(t *testing.T) {
		m := queuedModel(t)

		view := ansi.Strip(m.View().Content)

		if !strings.Contains(view, "💬 please also do X") || strings.Contains(shown(m), "please also do X") {
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
		if reply < 0 || asked < reply || strings.Contains(ansi.Strip(m.View().Content), "⧗ queued") {
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

	t.Run("QM-05 several queued show as one entry over the status line", func(t *testing.T) {
		m := queuedModel(t)
		m.input.SetValue("and keep it short")
		m = enter(t, m)

		view := ansi.Strip(m.View().Content)

		if strings.Count(view, "⧗ queued · 2") != 1 || !strings.Contains(view, "💬 and keep it short") || m.session.Agent.QueuedCount() != 2 {
			t.Errorf("view:\n%s", view)
		}
	})

	t.Run("QM-06 read by the model, several go into the conversation as one message", func(t *testing.T) {
		m := queuedModel(t)
		m.input.SetValue("and keep it short")
		m = enter(t, m)
		updated, _ := m.Update(streamEndMsg{})
		m = updated.(model)

		updated, cmd := m.Update(queuedReadMsg(2))
		m = deliver(updated.(model), cmd, 2)

		transcript := shown(m)
		first, second := strings.Index(transcript, "please also do X"), strings.Index(transcript, "and keep it short")
		if first < 0 || second < first || strings.Count(transcript, "❯") > 1 || len(m.queued) != 0 {
			t.Errorf("transcript:\n%s", transcript)
		}
	})

	t.Run("QM-07 up with nothing typed brings all that is queued back to the input", func(t *testing.T) {
		m := queuedModel(t)
		m.input.SetValue("and keep it short")
		m = enter(t, m)

		m = keys(t, m, keyUp)

		if m.input.Value() != "please also do X\n\nand keep it short" || len(m.queued) != 0 || m.session.Agent.QueuedCount() != 0 {
			t.Errorf("input %q queued %v waiting %d", m.input.Value(), m.queued, m.session.Agent.QueuedCount())
		}
		if view := ansi.Strip(m.View().Content); strings.Contains(view, "queued ›") {
			t.Errorf("still in view:\n%s", view)
		}
	})

	t.Run("QM-08 up with something typed, or with nothing queued, goes through the history", func(t *testing.T) {
		typed := queuedModel(t)
		typed.input.SetValue("half a thought")
		typed = keys(t, typed, keyUp)

		delivered := queuedModel(t)
		delivered.session.Agent.TakeQueued()
		delivered = keys(t, delivered, keyUp)

		if typed.session.Agent.QueuedCount() != 1 || len(typed.queued) != 1 || typed.input.Value() == "please also do X" {
			t.Errorf("typed: input %q queued %v waiting %d", typed.input.Value(), typed.queued, typed.session.Agent.QueuedCount())
		}
		if delivered.history.shown == 0 || delivered.session.Agent.QueuedCount() != 0 {
			t.Errorf("up did not go through the history: shown %d", delivered.history.shown)
		}
	})

	t.Run("QM-09 ctrl+x clears what is typed while the model works, and stops nothing", func(t *testing.T) {
		m := queuedModel(t)
		stopped := false
		m.cancel = func() { stopped = true }
		m.input.SetValue("something I changed my mind about")

		cleared := keys(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		again := keys(t, cleared, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})

		if cleared.input.Value() != "" || !cleared.running || stopped || len(cleared.queued) != 1 || !again.running {
			t.Errorf("input %q running %v stopped %v queued %v", cleared.input.Value(), cleared.running, stopped, cleared.queued)
		}
	})

	t.Run("QM-10 a long queued message wraps inside the main panel, never under the sidebar", func(t *testing.T) {
		m := queuedModel(t)
		m.width, m.height = 100, 30
		m.queued = []string{strings.Repeat("word ", 60)}

		rows := strings.Split(ansi.Strip(m.queuedView()), "\n")

		if len(rows) < 3 || !strings.HasPrefix(rows[0], "▌ 💬 word") || !strings.HasPrefix(rows[1], "▌    word") || !strings.Contains(rows[len(rows)-1], "⧗ queued · 1 · ↑ edit") {
			t.Fatalf("rows %q", rows)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > m.mainWidth() {
				t.Errorf("%d columns of %d: %q", ansi.StringWidth(row), m.mainWidth(), row)
			}
		}
	})

	t.Run("QM-11 a queued message of many lines is cut with a count of those not shown", func(t *testing.T) {
		m := queuedModel(t)
		m.queued = []string{strings.Repeat("line\n", 20), "last"}

		rows := strings.Split(ansi.Strip(m.queuedView()), "\n")

		if len(rows) != queuedRows+1 || !strings.Contains(rows[len(rows)-1], "… +13 lines") || !strings.Contains(rows[len(rows)-1], "↑ edit") {
			t.Errorf("rows %q", rows)
		}
	})

	t.Run("QM-12 the queue is shaded, in a colour neither the user's panel nor the chat has", func(t *testing.T) {
		m := queuedModel(t)

		drawn := m.queuedView()

		if queuedBackground(true) == panelBackground(true) || queuedBackground(false) == panelBackground(false) || !strings.Contains(drawn, "48;2;59;50;36") {
			t.Errorf("%q", drawn)
		}
	})

	t.Run("QM-13 the mark of a message follows the icon set, and an amber edge runs down the queue", func(t *testing.T) {
		m := queuedModel(t)
		for set, mark := range map[string]string{iconsEmoji: "💬", iconsText: "»", iconsNerd: "\uf075"} {
			m.session.Config.Render.Icons = set
			rows := strings.Split(m.queuedView(), "\n")
			if !strings.Contains(ansi.Strip(rows[0]), "▌ "+mark+" please") {
				t.Errorf("%s: %q", set, ansi.Strip(rows[0]))
			}
			for _, row := range rows {
				if !strings.Contains(row, "▌") || !strings.Contains(row, "224;175;104") {
					t.Errorf("%s: no amber edge in %q", set, row)
				}
			}
		}
	})
}
