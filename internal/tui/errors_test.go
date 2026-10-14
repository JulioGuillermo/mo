package tui

// Test Traceability Table
// | ID    | Category | Description                                                                   | Priority |
// | EF-01 | Happy    | A long error that ends a turn is folded to its first line, after the answer cut short | P0 |
// | EF-02 | Happy    | ctrl+o or a click opens a folded error whole, and folds it again               | P0       |
// | EF-03 | Edge     | A short error is shown whole, not folded                                        | P1       |

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var apiError = errors.New("400 Bad Request: [{\n  \"error\": {\n    \"code\": 400,\n    \"message\": \"Function call is missing a thought_signature in functionCall parts.\",\n    \"status\": \"INVALID_ARGUMENT\"\n  }\n}\n]")

func endedWith(t *testing.T, err error) model {
	t.Helper()
	m := conversationModel()
	m.running, m.cancel = true, func() {}
	m.stream = "I was about to"
	updated, _ := m.Update(runDoneMsg{err: err})
	return updated.(model)
}

func TestFoldedErrors(t *testing.T) {
	t.Run("EF-01 a long error that ends a turn is folded to its first line, after the answer cut short", func(t *testing.T) {
		m := endedWith(t, apiError)

		text := shown(m)

		if !strings.Contains(text, "✗ 400 Bad Request: [{ · 8 lines · ctrl+o or click to open") || strings.Contains(text, "thought_signature") {
			t.Errorf("shown:\n%s", text)
		}
		if strings.Index(text, "I was about to") > strings.Index(text, "400 Bad Request") {
			t.Errorf("the error came before the answer:\n%s", text)
		}
	})

	t.Run("EF-02 ctrl+o or a click opens a folded error whole, and folds it again", func(t *testing.T) {
		m := endedWith(t, apiError)

		opened := keys(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		closed := keys(t, opened, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		clicked := m
		for y := range m.conversationHeight() {
			probe := m
			if probe.clickDetails(y) && strings.Contains(shown(probe), "thought_signature") {
				clicked = probe
				break
			}
		}

		if !strings.Contains(shown(opened), "INVALID_ARGUMENT") || !strings.Contains(shown(opened), "ctrl+o or click to fold") {
			t.Errorf("opened:\n%s", shown(opened))
		}
		if strings.Contains(shown(closed), "INVALID_ARGUMENT") || !strings.Contains(shown(clicked), "INVALID_ARGUMENT") {
			t.Errorf("closed:\n%s\nclicked:\n%s", shown(closed), shown(clicked))
		}
	})

	t.Run("EF-03 a short error is shown whole, not folded", func(t *testing.T) {
		m := endedWith(t, errors.New("connection refused"))

		updated, _ := m.emitError(errors.New("no such file"))

		if text := shown(updated.(model)); !strings.Contains(text, "✗ no such file") || strings.Contains(text, "lines ·") {
			t.Errorf("shown:\n%s", text)
		}
	})
}
