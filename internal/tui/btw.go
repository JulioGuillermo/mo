package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

const (
	btwAsker       = "/btw"
	btwReadNote    = "[mo] A side question the user asks with /btw while the main task goes on. Answer it; you may only read. This exchange is not kept in the conversation."
	btwChangesNote = "[mo] A side task the user asks with /btw! while the main task goes on. Do it and answer briefly. This exchange is not kept in the conversation; the main task is told what you changed."
	btwUsage       = "/btw <question> asks aside with read-only tools · /btw! <task> with all its tools · /btw stops it"
)

// btwRun is a side question being answered, as /btw asks: the call it runs
// now, if any, and what stops it.
type btwRun struct {
	bang    bool
	started time.Time
	doing   string
	cancel  context.CancelFunc
}

// btwCallMsg is the summary of the call /btw runs now; empty once it ends.
type btwCallMsg string

// btwDoneMsg brings the answer of /btw, and for /btw! what it changed.
type btwDoneMsg struct {
	answer  string
	changes []string
	bang    bool
	err     error
}

// btwCommand asks aside, with the conversation so far and the tools of
// /btw, or of /btw! with bang; the main agent goes on undisturbed. Without
// a question it stops the one running.
func (m model) btwCommand(question string, bang bool) (tea.Model, tea.Cmd) {
	switch {
	case question == "" && m.btw != nil:
		m.btw.cancel()
		return m, nil
	case question == "":
		return m.emit(m.printer.Note(btwUsage))
	case m.btw != nil:
		return m.emit(m.printer.Note("/btw is answering already: /btw stops it"))
	case m.session.BtwTools == nil:
		return m.emit(m.printer.Note("/btw is not available here"))
	}
	history := m.session.Agent.History
	if m.running {
		history = m.stepHistory
	}
	aside := m.session.Agent.Aside(m.session.BtwTools(bang), history, !bang)
	start := len(aside.History)
	prompt := question + "\n\n" + btwReadNote
	if bang {
		prompt = question + "\n\n" + btwChangesNote
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.btw = &btwRun{bang: bang, started: time.Now(), cancel: cancel}
	hooks := m.btwHooks()
	ask := func() tea.Msg {
		err := aside.RunTyped(ctx, prompt, question, hooks)
		done := btwDoneMsg{answer: lastAnswer(aside.History[start:]), bang: bang, err: err}
		if bang {
			done.changes = aside.ChangesFrom(start)
		}
		return done
	}
	command := "/btw "
	if bang {
		command = "/btw! "
	}
	shown, _ := m.emit(m.printer.User(command + question))
	if m.running {
		return shown, ask
	}
	return shown, tea.Batch(ask, m.spinner.Tick)
}

// btwHooks show what /btw does in its own state line, and ask the user as
// the main agent does, saying who asks.
func (m model) btwHooks() agent.Hooks {
	bridge := m.bridge
	if bridge == nil {
		return agent.Hooks{}
	}
	return agent.Hooks{
		OnToolStart: func(call llm.ToolCall, summary string) {
			bridge.send(btwCallMsg(strings.TrimSpace(call.Name + " " + summary)))
		},
		OnToolDone: func(llm.ToolCall, string, bool) {
			bridge.send(btwCallMsg(""))
		},
		Interact: func(ctx context.Context, interaction agent.Interaction) agent.Reply {
			interaction.From = btwAsker
			return bridge.interact(ctx, interaction)
		},
	}
}

// btwDone shows the answer of /btw, kept out of the conversation, and tells
// the main agent what /btw! changed.
func (m model) btwDone(msg btwDoneMsg) (tea.Model, tea.Cmd) {
	if m.btw != nil {
		m.btw.cancel()
		m.btw = nil
	}
	var shown []string
	if msg.answer != "" {
		shown = append(shown, m.printer.Note("btw, not kept in the conversation:")+"\n"+m.printer.Answer(msg.answer))
	}
	if len(msg.changes) > 0 {
		m.session.Agent.Tell("While you worked, the user ran a side task with /btw! that did:\n- " + strings.Join(msg.changes, "\n- ") +
			"\nRead again what it changed before you rely on it.")
		shown = append(shown, m.printer.Note("btw! did, and the model is told: "+strings.Join(msg.changes, " · ")))
	}
	switch {
	case errors.Is(msg.err, context.Canceled):
		shown = append(shown, m.printer.Note("btw stopped"))
	case msg.err != nil:
		shown = append(shown, m.printer.Error(errors.Join(errors.New("btw"), msg.err)))
	case msg.answer == "":
		shown = append(shown, m.printer.Note("btw: no answer"))
	}
	return m.emit(strings.Join(shown, "\n"))
}

// lastAnswer is the text of the last reply in messages that has any.
func lastAnswer(messages []llm.Message) string {
	for _, message := range slices.Backward(messages) {
		if message.Role == llm.RoleAssistant && strings.TrimSpace(message.Text) != "" {
			return strings.TrimSpace(message.Text)
		}
	}
	return ""
}

// btwState says what /btw does, as the spinner says it for a turn.
func (m model) btwState() state {
	colors := statePalette(m.printer == nil || m.printer.isDark())
	word := "btw"
	if m.btw.bang {
		word = "btw!"
	}
	detail := m.btw.doing
	if detail == "" {
		detail = "waiting for the model"
	}
	return state{word: word, detail: detail, tint: colors.compacting}
}

// asker names who asks in an approval: a subagent by its label, or /btw.
func asker(from string) string {
	if from == btwAsker {
		return from
	}
	return "subagent " + from
}
