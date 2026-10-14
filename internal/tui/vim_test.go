package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VM-01 | Happy    | esc leaves writing; letters then move and edit instead of typing; i writes again | P0 |
// | VM-02 | Happy    | ctrl+k goes to the conversation; v j y copies lines; ctrl+j back to the input | P0  |
// | VM-03 | Happy    | ctrl+l goes to the panel; j moves between items; l opens the item's menu  | P1       |
// | VM-04 | Happy    | In settings lists l goes in and h goes back                               | P1       |
// | VM-05 | Happy    | /vim turns it on and off; off, keys type as always                        | P1       |
// | VM-06 | Happy    | l and h open and fold the block the cursor is on                          | P1       |
// | VM-07 | Happy    | u undoes an edit or a whole insert, ctrl+r redoes it                      | P1       |
// | VM-08 | Happy    | yy and dd go to the clipboard as lines; p pastes it below, P above, text inline | P0 |
// | VM-09 | Happy    | v in the input selects with the moves; y copies it, d deletes it         | P1       |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func vimKeys(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, key := range keys {
		var msg tea.KeyPressMsg
		switch {
		case key == "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case key == "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case strings.HasPrefix(key, "ctrl+"):
			msg = tea.KeyPressMsg{Code: rune(key[5]), Mod: tea.ModCtrl}
		default:
			msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
		}
		updated, _ := m.Update(msg)
		m = updated.(model)
	}
	return m
}

func vimModel(t *testing.T) model {
	t.Helper()
	m := sidebarModel(t, 140)
	m.vim = vimState{enabled: true}
	return m
}

func TestVim(t *testing.T) {
	t.Run("VM-01 esc leaves writing; letters then move and edit instead of typing; i writes again", func(t *testing.T) {
		m := vimKeys(t, vimModel(t), "h", "i", "!", "esc", "0", "x", "x", "A", "?")
		if m.input.Value() != "!?" {
			t.Fatalf("input = %q", m.input.Value())
		}
		m = vimKeys(t, m, "esc", "d", "d")
		if m.input.Value() != "" || m.vim.mode != vimNormal || !strings.Contains(ansi.Strip(m.lowerLayoutText()), "NORMAL") {
			t.Errorf("input = %q mode = %d", m.input.Value(), m.vim.mode)
		}
	})

	t.Run("VM-02 ctrl+k goes to the conversation; v j y copies lines; ctrl+j back to the input", func(t *testing.T) {
		m := vimModel(t)
		for _, line := range []string{"first line", "second line", "third line"} {
			updated, _ := m.emit(line)
			m = updated.(model)
		}
		m = vimKeys(t, m, "esc", "ctrl+k", "k", "v", "j")
		marked := ansi.Strip(m.conversation(m.conversationHeight()).View())
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		m = updated.(model)

		if !strings.Contains(marked, "second line") || cmd == nil || m.flash != "copied 2 lines" || m.vim.mode != vimNormal {
			t.Errorf("flash = %q mode = %d", m.flash, m.vim.mode)
		}
		if m = vimKeys(t, m, "ctrl+j"); m.vim.pane != paneInput {
			t.Errorf("pane = %d", m.vim.pane)
		}
	})

	t.Run("VM-03 ctrl+l goes to the panel; j moves between items; l opens the item's menu", func(t *testing.T) {
		m := vimKeys(t, vimModel(t), "esc", "ctrl+l", "j", "j")
		marked := m.sidebar(30)
		m = vimKeys(t, m, "l")

		if m.vim.pane != paneSidebar || m.vim.item != 2 || !strings.Contains(marked, vimPicked.Render("● 1 sleep 30")) {
			t.Errorf("pane = %d item = %d", m.vim.pane, m.vim.item)
		}
		if m.settings == nil || !strings.Contains(ansi.Strip(m.settings.form.View()), "Job 1") {
			t.Error("l did not open the job's menu")
		}
	})

	t.Run("VM-04 in settings lists l goes in and h goes back", func(t *testing.T) {
		m := vimKeys(t, vimModel(t), "esc", "ctrl+l", "j", "j", "l")
		back := vimKeys(t, m, "h")
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
		in := deliver(updated.(model), cmd, 3)

		if back.settings != nil {
			t.Error("h did not leave the menu")
		}
		if in.viewer == nil || in.viewer.id != 1 {
			t.Error("l did not go into the first option, watching the job")
		}
	})

	t.Run("VM-05 /vim turns it on and off; off, keys type as always", func(t *testing.T) {
		m := sidebarModel(t, 140)
		on, _ := m.runCommand("/vim")
		off, _ := on.(model).runCommand("/vim")
		typed := vimKeys(t, off.(model), "esc", "j")

		if !on.(model).vim.enabled || off.(model).vim.enabled || typed.input.Value() != "j" {
			t.Errorf("typed = %q", typed.input.Value())
		}
	})

	t.Run("VM-06 l and h open and fold the block the cursor is on", func(t *testing.T) {
		m := vimModel(t)
		m.addTool(llm.ToolCall{ID: "c1", Name: "read", Args: []byte(`{"path":"a.go"}`)}, "a.go")
		m.finishTool("c1", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight", false)
		m = vimKeys(t, m, "esc", "ctrl+k")
		tool := len(m.blocks) - 1
		_, foldable := m.transcript()
		for line, block := range foldable {
			if block == tool {
				m.vim.line = line
				break
			}
		}
		opened := vimKeys(t, m, "l")
		folded := vimKeys(t, opened, "h")

		if !opened.blocks[tool].open || folded.blocks[tool].open {
			t.Errorf("opened = %v folded = %v", opened.blocks[tool].open, folded.blocks[tool].open)
		}
	})

	t.Run("VM-07 u undoes an edit or a whole insert, ctrl+r redoes it", func(t *testing.T) {
		m := vimKeys(t, vimModel(t), "esc", "i", "a", "b", "c", "esc", "0", "x")
		if m.input.Value() != "bc" {
			t.Fatalf("input = %q", m.input.Value())
		}
		undone := vimKeys(t, m, "u")
		before := vimKeys(t, undone, "u")
		redone := vimKeys(t, before, "ctrl+r")

		if undone.input.Value() != "abc" || before.input.Value() != "" || redone.input.Value() != "abc" {
			t.Errorf("undone = %q before = %q redone = %q", undone.input.Value(), before.input.Value(), redone.input.Value())
		}
	})

	t.Run("VM-08 yy and dd go to the clipboard as lines; p pastes it below, P above, text inline", func(t *testing.T) {
		yanked := func(value string) model {
			m := vimModel(t)
			m.input.SetValue(value)
			m.vim.mode = vimNormal
			m.input.MoveToBegin()
			m.vim.register, m.vim.linewise = "one", true
			return m
		}
		m := yanked("one\ntwo")
		m.vim.register = ""
		updated, _, _ := m.inputKey("y")
		updated, cmd, _ := updated.(model).inputKey("y")
		if m = updated.(model); cmd == nil || m.vim.register != "one" || !m.vim.linewise {
			t.Fatalf("register = %q linewise = %v", m.vim.register, m.vim.linewise)
		}

		below, _ := yanked("one\ntwo").vimPasted(vimPasteMsg{text: "one", ok: true})
		above, _ := yanked("one\ntwo").vimPasted(vimPasteMsg{text: "zero\n", ok: true, before: true})
		inline, _ := yanked("ab").vimPasted(vimPasteMsg{text: "X", ok: true})
		offline, _ := yanked("ab").vimPasted(vimPasteMsg{ok: false})

		if got := below.(model).input.Value(); got != "one\none\ntwo" {
			t.Errorf("below = %q", got)
		}
		if got := above.(model).input.Value(); got != "zero\none\ntwo" {
			t.Errorf("above = %q", got)
		}
		if got := inline.(model).input.Value(); got != "aXb" {
			t.Errorf("inline = %q", got)
		}
		if got := offline.(model).input.Value(); got != "ab\none" {
			t.Errorf("without a clipboard the register is pasted: %q", got)
		}
	})

	t.Run("VM-09 v in the input selects with the moves; y copies it, d deletes it", func(t *testing.T) {
		m := vimKeys(t, vimModel(t), "esc", "i", "h", "e", "l", "l", "o", "esc", "0", "v", "l", "l")
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		copied := updated.(model)
		deleted := vimKeys(t, m, "d")

		if cmd == nil || copied.vim.register != "he" || copied.vim.mode != vimNormal {
			t.Errorf("register = %q mode = %d", copied.vim.register, copied.vim.mode)
		}
		if deleted.input.Value() != "llo" {
			t.Errorf("deleted = %q", deleted.input.Value())
		}
	})
}
