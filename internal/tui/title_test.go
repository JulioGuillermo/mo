package tui

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | TT-01 | Happy    | /title with a name renames the conversation and saves it              | P0       |
// | TT-02 | Happy    | /title auto takes the model's title; one for another conversation is not used | P0 |
// | TT-03 | Edge     | /title alone says the title; while the model works it waits for the turn's save | P1 |

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

// runAll runs a command's messages that are not printing, as the program would.
func runAll(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, each := range batch {
			m = runAll(t, m, each)
		}
		return m
	}
	switch msg.(type) {
	case titledMsg, savedMsg:
		updated, next := m.Update(msg)
		return runAll(t, updated.(model), next)
	}
	return m
}

func savedTitle(t *testing.T, m model) string {
	t.Helper()
	infos, _ := sessions.List(config.SessionDir(), config.ProjectKey(m.session.WorkDir))
	if len(infos) == 0 {
		return ""
	}
	return infos[0].Title
}

func TestTitle(t *testing.T) {
	t.Run("TT-01 /title with a name renames the conversation and saves it", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = slices.Clone(chat)

		updated, cmd := m.runCommand("/title Limpieza del repo")
		m = runAll(t, updated.(model), cmd)

		if m.thread.title != "Limpieza del repo" || savedTitle(t, m) != "Limpieza del repo" {
			t.Errorf("title %q saved %q", m.thread.title, savedTitle(t, m))
		}
	})

	t.Run("TT-02 /title auto takes the model's title; one for another conversation is not used", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = slices.Clone(chat)
		m.session.Agent.Provider = &replying{text: "Listar los archivos"}

		updated, cmd := m.runCommand("/title auto")
		m = runAll(t, updated.(model), cmd)
		stale, _ := m.Update(titledMsg{title: "Otra cosa", first: "something else"})

		if m.thread.title != "Listar los archivos" || savedTitle(t, m) != "Listar los archivos" {
			t.Errorf("title %q saved %q", m.thread.title, savedTitle(t, m))
		}
		if stale.(model).thread.title != "Listar los archivos" {
			t.Errorf("a stale title was used: %q", stale.(model).thread.title)
		}
	})

	t.Run("TT-03 /title alone says the title; while the model works it waits for the turn's save", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = slices.Clone(chat)
		m.thread.title = "Viejo"
		shownTitle, _ := m.runCommand("/title")
		m.running, m.cancel = true, func() {}

		updated, cmd := m.runCommand("/title Nuevo")
		m = runAll(t, updated.(model), cmd)

		if !strings.Contains(shown(shownTitle.(model)), "title: Viejo") {
			t.Errorf("shown: %q", shown(shownTitle.(model)))
		}
		if m.thread.title != "Nuevo" || savedTitle(t, m) != "" {
			t.Errorf("title %q saved %q", m.thread.title, savedTitle(t, m))
		}
	})
}

// replying answers every request with the same text.
type replying struct{ text string }

func (r *replying) Stream(_ context.Context, _ llm.Request, _ func(llm.Event)) (llm.Response, error) {
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: r.text}}, nil
}
