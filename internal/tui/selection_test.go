package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SL-01 | Happy    | Dragging copies the text dragged over, indent shared by its rows taken off | P0 |
// | SL-02 | Happy    | A selection stays in the column it started in, out of the side panel | P1       |
// | SL-03 | Happy    | While dragging the text shows selected; a click without a drag clicks | P1      |
// | SL-04 | Happy    | A drag held at the conversation's top scrolls it, and copies past the screen | P1 |
// | SL-05 | Happy    | A drag held at its bottom scrolls down; leaving the edge or letting go stops it | P1 |

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// cellOf finds where text shows on screen.
func cellOf(t *testing.T, m model, text string) point {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if x := strings.Index(line, text); x >= 0 {
			return point{ansi.StringWidth(line[:x]), y}
		}
	}
	t.Fatalf("%q is not on screen", text)
	return point{}
}

func dragged(t *testing.T, m model, from, to point) (model, tea.Cmd) {
	t.Helper()
	pressed, _ := m.Update(tea.MouseClickMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})
	moved, _ := pressed.(model).Update(tea.MouseMotionMsg{X: to.x, Y: to.y, Button: tea.MouseLeft})
	m = moved.(model)
	released, cmd := m.Update(tea.MouseReleaseMsg{X: to.x, Y: to.y, Button: tea.MouseLeft})
	return released.(model), cmd
}

func selectingModel(t *testing.T) model {
	t.Helper()
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	before := flashTime
	flashTime = time.Millisecond
	t.Cleanup(func() { flashTime = before })
	m := sidebarModel(t, 140)
	updated, _ := m.Update(printMsg(m.printer.Answer("```\nfunc main() {\n    run()\n}\n```")))
	return updated.(model)
}

func TestSelection(t *testing.T) {
	t.Run("SL-01 dragging copies the text dragged over, indent shared by its rows taken off", func(t *testing.T) {
		m := selectingModel(t)
		from, end := cellOf(t, m, "func main"), cellOf(t, m, "}")

		m, cmd := dragged(t, m, from, point{end.x, end.y})

		copied := sent(cmd)
		if !strings.Contains(copied, "func main() {\n    run()\n}") || !strings.Contains(ansi.Strip(m.statusLine()), "copied") {
			t.Errorf("copied = %q status = %q", copied, ansi.Strip(m.statusLine()))
		}
	})

	t.Run("SL-02 a selection stays in the column it started in, out of the side panel", func(t *testing.T) {
		m := selectingModel(t)
		from := cellOf(t, m, "func main")

		_, cmd := dragged(t, m, from, point{m.width - 1, from.y + 1})

		if copied := sent(cmd); strings.Contains(copied, "Tasks") || strings.Contains(copied, "read the code") || !strings.Contains(copied, "run()") {
			t.Errorf("copied = %q", copied)
		}
	})

	t.Run("SL-03 while dragging the text shows selected; a click without a drag clicks", func(t *testing.T) {
		m := selectingModel(t)
		from := cellOf(t, m, "func main")
		pressed, _ := m.Update(tea.MouseClickMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})
		moved, _ := pressed.(model).Update(tea.MouseMotionMsg{X: from.x + 3, Y: from.y, Button: tea.MouseLeft})

		view := moved.(model).View().Content
		clicked, cmd := m.Update(tea.MouseReleaseMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})

		if !strings.Contains(view, "\x1b[7mfunc\x1b[") && !strings.Contains(view, ";7mfunc") {
			t.Errorf("no reversed text in %q", fmt.Sprintf("%.200q", view))
		}
		if clicked.(model).flash != "" || strings.Contains(sent(cmd), "func") {
			t.Error("a click copied text")
		}
	})

	longModel := func(t *testing.T) model {
		t.Helper()
		m := selectingModel(t)
		var lines []string
		for i := 1; i <= 120; i++ {
			lines = append(lines, fmt.Sprintf("line %03d", i))
		}
		updated, _ := m.Update(printMsg(m.printer.Note(strings.Join(lines, "\n"))))
		return updated.(model)
	}
	hold := func(m model, ticks int) model {
		for range ticks {
			updated, _ := m.Update(edgeScrollMsg{})
			m = updated.(model)
		}
		return m
	}

	t.Run("SL-04 a drag held at the conversation's top scrolls it, and copies past the screen", func(t *testing.T) {
		m := longModel(t)
		from := cellOf(t, m, "line 120")
		from.x += len("line 120") - 1
		top := strings.Split(ansi.Strip(m.View().Content), "\n")[0]
		pressed, _ := m.Update(tea.MouseClickMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})
		moved, tick := pressed.(model).Update(tea.MouseMotionMsg{X: from.x, Y: 0, Button: tea.MouseLeft})
		m = hold(moved.(model), 10)
		scrolledTop := strings.Split(ansi.Strip(m.View().Content), "\n")[0]
		released, cmd := m.Update(tea.MouseReleaseMsg{X: from.x, Y: 0, Button: tea.MouseLeft})
		copied := sent(cmd)

		if tick == nil || scrolledTop == top || !released.(model).scrolled {
			t.Fatalf("it should scroll: top %q then %q", top, scrolledTop)
		}
		lines := strings.Split(copied, "\n")
		// copied ends with the flash's tick, as sent joins every message.
		if !strings.Contains(copied, "line 119\nline 120") || len(lines) < m.conversationHeight()+5 || !strings.Contains(copied, strings.TrimSpace(top)+"\n") {
			t.Errorf("copied %d lines: %.80q … %.40q (h %d)", len(lines), copied, copied[max(len(copied)-40, 0):], m.conversationHeight())
		}
	})

	t.Run("SL-05 a drag held at its bottom scrolls down; leaving the edge or letting go stops it", func(t *testing.T) {
		m := longModel(t)
		m.scroll(true, 40)
		from := cellOf(t, m, strings.TrimSpace(strings.Split(ansi.Strip(m.View().Content), "\n")[2]))
		before := m.conversationOffset()
		pressed, _ := m.Update(tea.MouseClickMsg{X: from.x, Y: from.y, Button: tea.MouseLeft})
		moved, _ := pressed.(model).Update(tea.MouseMotionMsg{X: from.x, Y: m.conversationHeight() + 2, Button: tea.MouseLeft})
		m = hold(moved.(model), 5)
		down := m.conversationOffset()
		left, _ := m.Update(tea.MouseMotionMsg{X: from.x, Y: 3, Button: tea.MouseLeft})
		stopped := hold(left.(model), 5)
		released, _ := stopped.Update(tea.MouseReleaseMsg{X: from.x, Y: 3, Button: tea.MouseLeft})
		_, after := released.(model).Update(edgeScrollMsg{})

		if down != before+5 {
			t.Errorf("offset %d then %d, want 5 lines down", before, down)
		}
		if stopped.conversationOffset() != down || after != nil {
			t.Errorf("it should stop: offset %d, a tick after release %v", stopped.conversationOffset(), after != nil)
		}
	})
}
