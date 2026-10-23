package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | NU-01 | Happy    | A turn ending while the terminal is not focused calls with the answer | P0      |
// | NU-02 | Happy    | Nothing is sent while focused, unless asked for, nor after esc        | P1       |
// | NU-03 | Happy    | A permission question calls with the tool, what it runs and how many more | P0  |
// | NU-04 | Happy    | The bell sound rings with the terminal's notification, off stays quiet | P0       |
// | NU-05 | Edge     | The bell rings once when the method is the bell too                   | P1       |

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// sent runs a command and returns what it would write to the terminal.
func sent(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var all []string
		for _, each := range batch {
			all = append(all, sent(each))
		}
		return strings.Join(all, "")
	}
	return fmt.Sprintf("%v", msg)
}

func notifyModel(t *testing.T, focused bool) model {
	t.Helper()
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TMUX", "")
	m := savedModel(t)
	m.session.Config = config.Defaults()
	m.session.Agent = &agent.Agent{History: []llm.Message{{Role: llm.RoleUser, Text: "go"}, {Role: llm.RoleAssistant, Text: "Should I also update the docs?"}}}
	m.focused = focused
	return m
}

func TestNotifications(t *testing.T) {
	t.Run("NU-01 a turn ending while the terminal is not focused calls with the answer", func(t *testing.T) {
		m := notifyModel(t, false)

		notified := sent(m.turnEnded(nil))

		if !strings.Contains(notified, "\x1b]777;notify;mo;Should I also update the docs?") {
			t.Errorf("sent = %q", notified)
		}
	})

	t.Run("NU-02 nothing is sent while focused, unless asked for, nor after esc", func(t *testing.T) {
		focused := notifyModel(t, true)
		always := notifyModel(t, true)
		always.session.Config.Notify.Focused = new(bool)
		*always.session.Config.Notify.Focused = true
		blurred := notifyModel(t, false)
		blurred.running, blurred.cancel = true, func() {}

		_, afterEsc := blurred.finishRun(context.Canceled)

		if focused.turnEnded(nil) != nil || always.turnEnded(nil) == nil || strings.Contains(sent(afterEsc), "777") {
			t.Error("focus or esc was not respected")
		}
	})

	t.Run("NU-03 a permission question calls with the tool and what it runs", func(t *testing.T) {
		m := notifyModel(t, false)
		question := interactionMsg{interaction: agent.Interaction{
			Approvals: []agent.ApprovalRequest{{Call: llm.ToolCall{Name: "bash"}, Summary: "rm -rf build"}},
			Questions: []tools.Question{{Text: "Which branch?"}},
		}, reply: make(chan agent.Reply, 1)}

		_, cmd := m.Update(question)

		if !strings.Contains(sent(cmd), "Run bash? rm -rf build (+1 more)") {
			t.Errorf("sent = %q", sent(cmd))
		}
	})

	t.Run("NU-04 the bell sound rings with the terminal's notification, off stays quiet", func(t *testing.T) {
		quiet := notifyModel(t, false)
		ringing := notifyModel(t, false)
		ringing.session.Config.Notify.Sound = "bell"

		silent, rung := sent(quiet.turnEnded(nil)), sent(ringing.turnEnded(nil))

		if strings.Count(silent, "\a") != 1 || strings.Count(rung, "\a") != 2 || !strings.Contains(rung, "777;notify") {
			t.Errorf("off = %q bell = %q", silent, rung)
		}
	})

	t.Run("NU-05 the bell rings once when the method is the bell too", func(t *testing.T) {
		m := notifyModel(t, false)
		m.session.Config.Notify.Method, m.session.Config.Notify.Sound = "bell", "bell"

		if rung := sent(m.turnEnded(nil)); strings.Count(rung, "\a") != 1 {
			t.Errorf("sent = %q", rung)
		}
	})
}
