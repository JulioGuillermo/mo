package tui

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | HI-01 | Happy    | Up on an empty input goes back through what was sent; down comes forward, past the newest to empty | P0 |
// | HI-02 | Edge     | A command from the history is gone through too; the suggestions do not take the arrows | P1 |
// | HI-03 | Happy    | Up from what is typed goes to the history; down past the newest brings it back | P0 |
// | HI-06 | Edge     | Up below the first line of what is typed moves in it; edited, an entry is the draft | P1 |
// | HI-07 | Edge     | A command's suggestions keep the arrows                                    | P1       |
// | HI-04 | Happy    | What is sent is kept between sessions, a repeat once, several lines whole  | P0       |
// | HI-05 | Happy    | An entry of several lines is gone through line by line before the next one, up and down | P0 |

import (
	"os"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var keyUp = tea.KeyPressMsg{Code: tea.KeyUp}

func historyModel(t *testing.T, entries ...string) model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := conversationModel()
	m.history = inputHistory{entries: entries}
	return m
}

func TestInputHistory(t *testing.T) {
	t.Run("HI-01 up on an empty input goes back through what was sent; down comes forward, past the newest to empty", func(t *testing.T) {
		m := historyModel(t, "first", "second", "third")
		var seen []string
		for _, key := range []tea.KeyPressMsg{keyUp, keyUp, keyUp, keyUp, keyDown, keyDown, keyDown} {
			m = keys(t, m, key)
			seen = append(seen, m.input.Value())
		}

		want := []string{"third", "second", "first", "first", "second", "third", ""}
		if !slices.Equal(seen, want) {
			t.Errorf("seen %q", seen)
		}
	})

	t.Run("HI-02 a command from the history is gone through too; the suggestions do not take the arrows", func(t *testing.T) {
		m := historyModel(t, "fix the tests", "/settings", "/model")

		m = keys(t, m, keyUp, keyUp, keyUp)

		if m.input.Value() != "fix the tests" {
			t.Errorf("input %q", m.input.Value())
		}
	})

	t.Run("HI-03 up from what is typed goes to the history; down past the newest brings it back", func(t *testing.T) {
		m := historyModel(t, "older", "newer")
		m.input.SetValue("my draft")

		m = keys(t, m, keyUp, keyUp)
		up := m.input.Value()
		m = keys(t, m, keyDown, keyDown)

		if up != "older" || m.input.Value() != "my draft" || m.history.draft != "" {
			t.Errorf("up %q, back %q", up, m.input.Value())
		}
	})

	t.Run("HI-06 up below the first line of what is typed moves in it; edited, an entry is the draft", func(t *testing.T) {
		m := historyModel(t, "older", "newer")
		m.input.SetValue("one\ntwo")

		m = keys(t, m, keyUp)
		moved := m.input.Value() == "one\ntwo" && m.input.Line() == 0
		m.input.Reset()
		m = keys(t, m, keyUp, tea.KeyPressMsg{Code: 'x', Text: "x"}, keyUp, keyDown)

		if !moved || m.input.Value() != "newerx" {
			t.Errorf("moved %v, after edit %q", moved, m.input.Value())
		}
	})

	t.Run("HI-07 a command's suggestions keep the arrows", func(t *testing.T) {
		m := historyModel(t, "older")
		m.input.SetValue("/mo")

		m = keys(t, m, keyUp)

		if m.input.Value() != "/mo" || m.history.shown != 0 {
			t.Errorf("input %q shown %d", m.input.Value(), m.history.shown)
		}
	})

	t.Run("HI-04 what is sent is kept between sessions, a repeat once, several lines whole", func(t *testing.T) {
		m := historyModel(t)
		for _, entry := range []string{"line one\nline two", "/help", "/help"} {
			m.input.SetValue(entry)
			updated, cmd := m.submit()
			m = updated.(model)
			if cmd != nil {
				runCmds(cmd)
			}
		}

		info, _ := os.Stat(historyFile())
		back := loadHistory()
		if !slices.Equal(back.entries, []string{"line one\nline two", "/help"}) || info.Mode().Perm() != 0o600 {
			t.Errorf("kept %q", back.entries)
		}
	})
}

func TestInputHistoryLines(t *testing.T) {
	t.Run("HI-05 an entry of several lines is gone through line by line before the next one, up and down", func(t *testing.T) {
		m := historyModel(t, "older", "a\nb\nc", "newer")
		type place struct {
			value string
			line  int
		}
		var seen []place
		for _, key := range []tea.KeyPressMsg{keyUp, keyUp, keyUp, keyUp, keyUp, keyDown, keyDown, keyDown, keyDown, keyDown, keyDown} {
			m = keys(t, m, key)
			seen = append(seen, place{m.input.Value(), m.input.Line()})
		}

		want := []place{
			{"newer", 0}, {"a\nb\nc", 2}, {"a\nb\nc", 1}, {"a\nb\nc", 0}, {"older", 0},
			{"a\nb\nc", 0}, {"a\nb\nc", 1}, {"a\nb\nc", 2}, {"newer", 0}, {"", 0}, {"", 0},
		}
		if !slices.Equal(seen, want) {
			t.Errorf("seen %v", seen)
		}
	})
}

// runCmds runs a command and those it batches, for what they do, not what
// they say.
func runCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, each := range batch {
			runCmds(each)
		}
	}
}
