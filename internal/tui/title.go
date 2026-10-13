package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/sessions"
)

const autoTitle = "auto"

// titledMsg brings the title the model suggested.
type titledMsg struct {
	title string
	err   error
	// first is the conversation's first prompt when it was asked, so a
	// title that comes after /clear or /resume is not given to another.
	first string
}

// retitle names the conversation: with the text given, or by the model
// with auto. Without either it says the title it has.
func (m model) retitle(argument string) (tea.Model, tea.Cmd) {
	argument = strings.TrimSpace(argument)
	switch {
	case argument == "":
		current := m.thread.title
		if current == "" {
			current = "none yet: the first prompt names it"
		}
		return m.emit(m.printer.Note("title: " + current + " · /title <name>, or /title auto"))
	case argument == autoTitle:
		if len(m.session.Agent.Prompts()) == 0 {
			return m.emit(m.printer.Note("nothing to title yet: write a prompt first"))
		}
		assistant := m.session.Agent
		first := m.firstPrompt()
		note := printText(m.printer.Note("asking the model for a title…"))
		return m, tea.Batch(note, func() tea.Msg {
			title, err := assistant.SuggestTitle(context.Background())
			return titledMsg{title: title, err: err, first: first}
		})
	}
	return m.setTitle(sessions.Title(argument))
}

func (m model) titled(msg titledMsg) (tea.Model, tea.Cmd) {
	switch {
	case errors.Is(msg.err, agent.ErrNothingToTitle):
		return m.emit(m.printer.Note("nothing to title yet: write a prompt first"))
	case msg.err != nil:
		return m.emit(m.printer.Error(errors.Join(errors.New("no title"), msg.err)))
	case msg.first != m.firstPrompt():
		return m.emit(m.printer.Note("the title came for another conversation: " + msg.title))
	}
	return m.setTitle(sessions.Title(msg.title))
}

// setTitle keeps the title for the conversation and saves it, or leaves the
// saving to the end of the turn while the model works.
func (m model) setTitle(title string) (tea.Model, tea.Cmd) {
	m.thread.title = title
	note := printText(m.printer.Note("title: " + title))
	if m.running {
		return m, note
	}
	save := m.saveConversation()
	return m, tea.Batch(note, save)
}

func (m model) firstPrompt() string {
	if prompts := m.session.Agent.Prompts(); len(prompts) > 0 {
		return prompts[0].Text
	}
	return ""
}
