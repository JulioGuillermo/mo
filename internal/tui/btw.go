package tui

import (
	"context"
	"errors"
	"fmt"
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
	btwUsage       = "/btw <question> asks aside with read-only tools · /btw! <task> with all its tools · /btw open shows the last answer in a box · /btw stops it"
)

// btwAnswer is a side question and what came of it, shared by its block in
// the conversation and the box that shows it, both drawn again as it
// streams.
type btwAnswer struct {
	question  string
	bang      bool
	text      string
	reasoning string
	done      bool
	// ending says how it ended when not with an answer: stopped, failed.
	ending  string
	changes []string
	// dirty is set when it changed since it was last drawn; drawn is its
	// body as last drawn, from that text and at that width.
	dirty      bool
	drawn      []string
	drawnText  string
	drawnWidth int
}

// btwRun is a side question being answered, as /btw asks: the call it runs
// now, if any, and what stops it.
type btwRun struct {
	answer  *btwAnswer
	started time.Time
	doing   string
	cancel  context.CancelFunc
}

// btwEventMsg is a piece of the answer of /btw as it streams.
type btwEventMsg llm.Event

// btwCallMsg is the summary of the call /btw runs now; empty once it ends.
type btwCallMsg string

// btwDoneMsg brings how /btw ended, and for /btw! what it changed.
type btwDoneMsg struct {
	answer  string
	changes []string
	err     error
}

// btwCommand asks aside, with the conversation so far, the files attached
// and the tools of /btw, or of /btw! with bang; the main agent goes on
// undisturbed. Without a question it stops the one running; open shows
// the last answer in a box.
func (m model) btwCommand(question string, bang bool) (tea.Model, tea.Cmd) {
	switch {
	case question == "" && m.btw != nil:
		m.btw.cancel()
		return m, nil
	case question == "":
		return m.emit(m.printer.Note(btwUsage))
	case question == "open" && !bang:
		return m.openLastBtw()
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
	prompt := m.withAttachments(question) + "\n\n" + btwReadNote
	if bang {
		prompt = strings.TrimSuffix(prompt, btwReadNote) + btwChangesNote
	}
	ctx, cancel := context.WithCancel(context.Background())
	answer := &btwAnswer{question: question, bang: bang}
	m.btw = &btwRun{answer: answer, started: time.Now(), cancel: cancel}
	m.addBtwBlock(answer)
	hooks := m.btwHooks()
	ask := func() tea.Msg {
		err := aside.RunTyped(ctx, prompt, question, hooks)
		done := btwDoneMsg{answer: lastAnswer(aside.History[start:]), err: err}
		if bang {
			done.changes = aside.ChangesFrom(start)
		}
		return done
	}
	if m.running {
		return m, ask
	}
	return m, tea.Batch(ask, m.spinner.Tick)
}

// btwHooks bring what /btw writes and runs to its block and its line over
// the input, and ask the user as the main agent does, saying who asks.
func (m model) btwHooks() agent.Hooks {
	bridge := m.bridge
	if bridge == nil {
		return agent.Hooks{}
	}
	return agent.Hooks{
		OnEvent: func(event llm.Event) {
			if event.Kind == llm.EventText || event.Kind == llm.EventReasoning {
				bridge.send(btwEventMsg(event))
			}
		},
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

// btwStreamed adds a piece of the answer, drawn at the next tick of the
// spinner.
func (m model) btwStreamed(event llm.Event) (tea.Model, tea.Cmd) {
	if m.btw == nil {
		return m, nil
	}
	answer := m.btw.answer
	switch event.Kind {
	case llm.EventText:
		answer.text += event.Text
	case llm.EventReasoning:
		answer.reasoning += event.Text
	}
	answer.dirty = true
	return m, nil
}

// btwCalling says the call /btw runs now. A reply that calls tools ends
// there, so the text before it starts over with the next.
func (m model) btwCalling(summary string) (tea.Model, tea.Cmd) {
	if m.btw == nil {
		return m, nil
	}
	m.btw.doing = summary
	if summary != "" {
		m.btw.answer.text, m.btw.answer.reasoning = "", ""
		m.btw.answer.dirty = true
	}
	return m, nil
}

// btwDone shows how /btw ended in its block, and tells the main agent what
// /btw! changed.
func (m model) btwDone(msg btwDoneMsg) (tea.Model, tea.Cmd) {
	if m.btw == nil {
		return m, nil
	}
	m.btw.cancel()
	answer := m.btw.answer
	m.btw = nil
	answer.text, answer.done, answer.changes = msg.answer, true, msg.changes
	switch {
	case errors.Is(msg.err, context.Canceled):
		answer.ending = "stopped"
	case msg.err != nil:
		answer.ending = "failed: " + msg.err.Error()
	case msg.answer == "":
		answer.ending = "no answer"
	}
	if len(msg.changes) > 0 {
		m.session.Agent.Tell("While you worked, the user ran a side task with /btw! that did:\n- " + strings.Join(msg.changes, "\n- ") +
			"\nRead again what it changed before you rely on it.")
	}
	m.redrawBtw(answer)
	return m, nil
}

// redrawBtw draws the block of an answer again, as it is now.
func (m *model) redrawBtw(answer *btwAnswer) {
	answer.dirty = false
	m.blocks = slices.Clone(m.blocks)
	for i := range m.blocks {
		if m.blocks[i].btw == answer {
			m.blocks[i].redraw(m.blocks[i].open)
		}
	}
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
	detail := m.btw.doing
	switch {
	case detail != "":
	case m.btw.answer.text != "":
		detail = "writing"
	case m.btw.answer.reasoning != "":
		detail = "reasoning"
	default:
		detail = "waiting for the model"
	}
	return state{word: btwWord(m.btw.answer), detail: detail, tint: colors.compacting}
}

// btwStateLine is the line over the input while /btw answers; a click on it
// opens its answer in a box.
func (m model) btwStateLine() (string, []region) {
	elapsed := " · " + formatElapsed(time.Since(m.btw.started))
	tail := elapsed + " · click to see it · /btw stops it"
	if !m.running && m.dialog == nil {
		tail = elapsed + " · click to see it · esc to stop"
	}
	answer := m.btw.answer
	return m.stateLine(m.btwState(), tail), []region{row(0, func(m model) (tea.Model, tea.Cmd) { return m.openBtwBox(answer) })}
}

func btwWord(answer *btwAnswer) string {
	if answer.bang {
		return "btw!"
	}
	return "btw"
}

// asker names who asks in an approval: a subagent by its label, or /btw.
func asker(from string) string {
	if from == btwAsker {
		return from
	}
	return "subagent " + from
}

// btwLines counts the lines of an answer, to say how long it is folded.
func btwLines(answer *btwAnswer) string {
	lines := strings.Count(strings.TrimSpace(answer.text), "\n") + 1
	if lines == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", lines)
}
