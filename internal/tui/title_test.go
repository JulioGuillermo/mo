package tui

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | TT-01 | Happy    | /title with a name renames the conversation and saves it              | P0       |
// | TT-02 | Happy    | /title auto takes the model's title; one for another conversation is not used | P0 |
// | TT-03 | Edge     | /title alone says the title; while the model works it waits for the turn's save | P1 |
// | TT-04 | Happy    | /title auto shows a spinner saying where the model is; esc stops it    | P1       |
// | TT-05 | Happy    | Under the spinner, the last two lines of what the model writes for the title | P1  |
// | TT-06 | Happy    | The title tool renames the conversation as /title does                  | P0       |

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

func TestTitleProgress(t *testing.T) {
	t.Run("TT-04 /title auto shows a spinner saying where the model is; esc stops it", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = slices.Clone(chat)
		updated, _ := m.runCommand("/title auto")
		m = updated.(model)
		upper := func(m model) string {
			text, _ := m.upperLayout()
			return ansi.Strip(text)
		}
		waiting := upper(m)
		updated, _ = m.Update(titleEventMsg(llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 50, Total: 100}}))
		reading := upper(updated.(model))
		updated, _ = updated.(model).Update(titleEventMsg(llm.Event{Kind: llm.EventReasoning, Text: "hmm"}))
		reasoning := upper(updated.(model))
		watch := contextOf(updated.(model).titling)

		stopped, _ := updated.(model).Update(keyEsc)
		done, _ := stopped.(model).Update(titledMsg{err: context.Canceled, first: "list the files"})

		if !strings.Contains(waiting, "titling · waiting for the model · esc to stop") {
			t.Errorf("waiting:\n%s", waiting)
		}
		if !strings.Contains(reading, "titling · prompt 50%") || !strings.Contains(reasoning, "titling · reasoning") {
			t.Errorf("reading:\n%s\nreasoning:\n%s", reading, reasoning)
		}
		select {
		case <-watch.Done():
		default:
			t.Error("esc did not stop the title")
		}
		if done.(model).titling != nil || !strings.Contains(shown(done.(model)), "title not changed") || strings.Contains(upper(done.(model)), "titling") {
			t.Error("the title did not end")
		}
	})
}

func TestTitleShown(t *testing.T) {
	t.Run("TT-05 under the spinner, the last two lines of what the model writes for the title", func(t *testing.T) {
		m := savedModel(t)
		m.width = 44
		m.session.Agent.History = slices.Clone(chat)
		updated, _ := m.runCommand("/title auto")
		long := strings.Repeat("palabra ", 30) + "<title>Listar archivos</title>"

		updated, _ = updated.(model).Update(titleEventMsg(llm.Event{Kind: llm.EventText, Text: long}))

		text, _ := updated.(model).upperLayout()
		lines := strings.Split(ansi.Strip(text), "\n")
		if len(lines) != 3 || !strings.Contains(lines[2], "<title>Listar archivos</title>") || !strings.HasPrefix(lines[1], "  palabra") {
			t.Errorf("shown:\n%s", strings.Join(lines, "\n"))
		}
	})

	t.Run("TT-06 the title tool renames the conversation as /title does", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = slices.Clone(chat)

		updated, cmd := m.Update(titleSetMsg("Repaso   de archivos"))
		m = runAll(t, updated.(model), cmd)

		if m.thread.title != "Repaso   de archivos" || savedTitle(t, m) != m.thread.title {
			t.Errorf("title %q saved %q", m.thread.title, savedTitle(t, m))
		}
	})
}

// contextOf is a context cancelled with the title, to see esc stop it.
func contextOf(t *titling) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	stop := t.cancel
	t.cancel = func() { stop(); cancel() }
	return ctx
}

// replying answers every request with the same text.
type replying struct{ text string }

func (r *replying) Stream(_ context.Context, _ llm.Request, _ func(llm.Event)) (llm.Response, error) {
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: r.text}}, nil
}
