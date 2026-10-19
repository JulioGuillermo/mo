package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

const autoTitle = "auto"

// titling is a title being asked for: how far the model's reply is, and
// what stops it.
type titling struct {
	cancel context.CancelFunc
	last   llm.EventKind
	// started is set once the reply streams anything.
	started bool
	read    llm.PromptProgress
	// said is the end of what the model has written or reasoned so far.
	said string
}

// titleShownLength bounds what is kept of the title's reply to show.
const titleShownLength = 600

// titleEventMsg is a piece of the title's reply as it streams.
type titleEventMsg llm.Event

func (t *titling) follow(event llm.Event) {
	t.last, t.started = event.Kind, true
	switch event.Kind {
	case llm.EventPromptProgress:
		t.read = event.Progress
	case llm.EventText, llm.EventReasoning:
		said := []rune(t.said + event.Text)
		t.said = string(said[max(0, len(said)-titleShownLength):])
	}
}

// saidLines are the last two lines of what the model writes for the title,
// dim, under its spinner.
func (m model) saidLines() string {
	said := strings.Join(strings.Fields(m.titling.said), " ")
	if said == "" {
		return ""
	}
	width := max(m.mainWidth()-4, 20)
	lines := strings.Split(ansi.Hardwrap(said, width, true), "\n")
	lines = lines[max(0, len(lines)-2):]
	for i, line := range lines {
		lines[i] = "  " + dimStyle.Render(strings.TrimSpace(line))
	}
	return strings.Join(lines, "\n")
}

// titleState says what the title waits on, as the spinner says it for a turn.
func (m model) titleState() state {
	colors := statePalette(m.printer == nil || m.printer.isDark())
	detail := "waiting for the model"
	switch {
	case !m.titling.started:
	case m.titling.last == llm.EventPromptProgress:
		detail = "prompt " + promptDetail(m.titling.read)
	case m.titling.last == llm.EventReasoning:
		detail = "reasoning"
	default:
		detail = "writing"
	}
	return state{word: "titling", detail: detail, tint: colors.compacting}
}

// titleSetMsg is a title the agent gave the conversation with its tool.
type titleSetMsg string

// titledMsg brings the title the model suggested.
type titledMsg struct {
	title string
	err   error
	// first is the conversation's first prompt when it was asked, so a
	// title that comes after /new or /resume is not given to another.
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
		if m.titling != nil {
			return m.emit(m.printer.Note("the model is already working out a title"))
		}
		assistant := m.session.Agent
		first := m.firstPrompt()
		ctx, cancel := context.WithCancel(context.Background())
		m.titling = &titling{cancel: cancel}
		bridge := m.bridge
		ask := func() tea.Msg {
			title, err := assistant.SuggestTitle(ctx, func(event llm.Event) {
				if bridge != nil && event.Kind != llm.EventToolCall {
					bridge.send(titleEventMsg(event))
				}
			})
			return titledMsg{title: title, err: err, first: first}
		}
		if m.running {
			return m, ask
		}
		return m, tea.Batch(ask, m.spinner.Tick)
	}
	return m.setTitle(sessions.Title(argument))
}

func (m model) titled(msg titledMsg) (tea.Model, tea.Cmd) {
	if m.titling != nil {
		m.titling.cancel()
		m.titling = nil
	}
	switch {
	case errors.Is(msg.err, context.Canceled):
		return m.emit(m.printer.Note("title not changed"))
	case errors.Is(msg.err, agent.ErrNothingToTitle):
		return m.emit(m.printer.Note("nothing to title yet: write a prompt first"))
	case msg.err != nil:
		return m.emitError(errors.Join(errors.New("no title"), msg.err))
	case msg.first != m.firstPrompt():
		return m.emit(m.printer.Note("the title came for another conversation: " + msg.title))
	}
	return m.setTitle(sessions.Title(msg.title))
}

// setTitle keeps the title for the conversation and saves it, or leaves the
// saving to the end of the turn while the model works.
func (m model) setTitle(title string) (tea.Model, tea.Cmd) {
	m.thread.title = title
	m.tellPeers()
	note := tea.Batch(printText(m.printer.Note("title: "+title)), m.renamePeer())
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
