package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | RS-01 | Happy    | A reasoning shows as one line, then whole after ctrl+o, and folds again | P0    |
// | RS-02 | Happy    | A click on the line of a reasoning opens only that one               | P1       |
// | RS-03 | Edge     | A run stopped while thinking keeps what was thought, folded           | P2       |
// | RS-04 | Happy    | Sending reasoning back is saved only when turned on                  | P1       |
// | RZ-01 | Happy    | After a resize what was printed is drawn again at the new width, once | P0      |
// | RZ-02 | Edge     | Output that uploads kitty images is not drawn again                  | P1       |

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
)

func conversationModel() model {
	m, _ := menuModel()
	m.settings = nil
	return m
}

func shown(m model) string {
	text, _ := m.transcript()
	return ansi.Strip(text)
}

func TestReasoningBlocks(t *testing.T) {
	t.Run("RS-01 a reasoning shows as one line, then whole after ctrl+o, and folds again", func(t *testing.T) {
		m := conversationModel()
		updated, _ := m.Update(reasoningMsg("first idea\nsecond idea"))
		m = updated.(model)
		folded := shown(m)

		opened, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		refolded, _ := opened.(model).Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

		if !strings.Contains(folded, "▸ reasoning · 2 lines") || strings.Contains(folded, "second idea") {
			t.Errorf("folded:\n%s", folded)
		}
		if !strings.Contains(shown(opened.(model)), "second idea") || strings.Contains(shown(refolded.(model)), "second idea") {
			t.Errorf("opened:\n%s\nrefolded:\n%s", shown(opened.(model)), shown(refolded.(model)))
		}
	})

	t.Run("RS-02 a click on the line of a reasoning opens only that one", func(t *testing.T) {
		m := conversationModel()
		for _, msg := range []tea.Msg{printMsg("hello"), reasoningMsg("alpha"), printMsg("answer"), reasoningMsg("beta")} {
			updated, _ := m.Update(msg)
			m = updated.(model)
		}
		row := -1
		lines := strings.Split(shown(m), "\n")
		for i, line := range lines {
			if strings.Contains(line, "▸") {
				row = i
				break
			}
		}
		conversation := m.conversation(m.height - 1)
		row -= conversation.YOffset()

		updated, _ := m.Update(tea.MouseClickMsg{X: 3, Y: row, Button: tea.MouseLeft})

		after := shown(updated.(model))
		if !strings.Contains(after, "alpha") || strings.Contains(after, "beta\n") || updated.(model).blocks[3].open {
			t.Errorf("after the click:\n%s", after)
		}
	})

	t.Run("RS-03 a run stopped while thinking keeps what was thought, folded", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m.reasoning = "half a thought"

		updated, _ := m.finishRun(errors.New("boom"))

		after := updated.(model)
		if len(after.blocks) != 1 || after.blocks[0].reasoning != "half a thought" || after.blocks[0].open {
			t.Errorf("blocks = %+v", after.blocks)
		}
	})

	t.Run("RS-04 sending reasoning back is saved only when turned on", func(t *testing.T) {
		file := config.Config{Provider: "llama-server"}
		untouched, _ := newSettingsDraft(file).applyTo(file, nil)
		draft := newSettingsDraft(file)
		draft.keepReasoning = true
		on, _ := draft.applyTo(file, nil)

		if untouched.Context.KeepReasoning != nil || on.Context.KeepReasoning == nil || !*on.Context.KeepReasoning {
			t.Errorf("untouched = %v on = %v", untouched.Context.KeepReasoning, on.Context.KeepReasoning)
		}
	})
}

func TestResize(t *testing.T) {
	t.Run("RZ-01 after a resize what was printed is drawn again at the new width, once", func(t *testing.T) {
		m := conversationModel()
		m.width = 80
		m.printer.setWidth(80)
		updated, _ := m.Update(drawMsg(func() string { return m.printer.User("hi") }))
		m = updated.(model)
		before := ansi.StringWidth(m.blocks[0].text)

		first, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		second, _ := first.(model).Update(tea.WindowSizeMsg{Width: 140, Height: 40})
		stale, _ := second.(model).Update(redrawMsg(1))
		staleWidth := ansi.StringWidth(stale.(model).blocks[0].text)
		redrawn, _ := stale.(model).Update(redrawMsg(second.(model).redraws))

		if before != 80 || staleWidth != 80 || ansi.StringWidth(strings.Split(redrawn.(model).blocks[0].text, "\n")[1]) != redrawn.(model).mainWidth() {
			t.Errorf("before = %d stale = %d after = %q", before, staleWidth, redrawn.(model).blocks[0].text)
		}
	})

	t.Run("RZ-02 output that uploads kitty images is not drawn again", func(t *testing.T) {
		m := conversationModel()

		updated, cmd := m.Update(drawMsg(func() string { return "\x1b_Ga=t;AAAA\x1b\\picture" }))

		if updated.(model).blocks[0].draw != nil || cmd == nil || updated.(model).blocks[0].text != "picture" {
			t.Errorf("block = %+v", updated.(model).blocks[0])
		}
	})
}
