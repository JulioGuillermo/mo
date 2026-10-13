package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

const liveReasoningLines = 3

// block is a piece of the transcript. draw draws it again, at a new width or
// opened or folded; without it the text stays as printed. A foldable block
// (a reasoning, a tool call, a summary) opens with a click or ctrl+o.
type block struct {
	text      string
	draw      func(open bool) string
	foldable  bool
	open      bool
	reasoning string
	tool      *toolRun
}

// toolRun is a tool call shown in the transcript, filled in when it ends.
type toolRun struct {
	id, name, summary, output string
	isError, done             bool
	args                      json.RawMessage
}

type reasoningMsg string

// drawMsg prints what draw returns, and draws it again at a new width.
type drawMsg func() string

type toolStartMsg struct {
	call    llm.ToolCall
	summary string
}

type toolDoneMsg struct {
	id      string
	output  string
	isError bool
}

// redrawMsg asks for the redraw it numbers, done only if no resize came
// after it.
type redrawMsg int

const redrawDelay = 120 * time.Millisecond

func drawText(draw func() string) tea.Cmd {
	return func() tea.Msg { return drawMsg(draw) }
}

// emitDrawn adds a block that can be drawn again. One that uploads kitty
// images stays as first drawn, so the images are sent only once.
func (m model) emitDrawn(draw drawMsg) (tea.Model, tea.Cmd) {
	text := draw()
	if kittyUploads.MatchString(text) {
		return m.emit(text)
	}
	m.blocks = append(m.blocks, block{text: text, draw: func(bool) string { return draw() }})
	return m, nil
}

// addFoldable adds a block that opens and folds, open or not as the last
// ctrl+o left them.
func (m *model) addFoldable(b block) {
	b.foldable, b.open = true, m.showDetails
	b.text = b.draw(b.open)
	m.blocks = append(m.blocks, b)
}

// addReasoning adds a reasoning of the model, folded to one line.
func (m *model) addReasoning(text string) {
	if text = strings.TrimSpace(text); text != "" {
		printer := m.printer
		m.addFoldable(block{reasoning: text, draw: func(open bool) string { return printer.fold("reasoning", text, open, false) }})
	}
}

// addTool adds a tool call, its result to come.
func (m *model) addTool(call llm.ToolCall, summary string) {
	run := &toolRun{id: call.ID, name: call.Name, summary: summary, args: call.Args}
	printer := m.printer
	m.addFoldable(block{tool: run, draw: func(open bool) string { return printer.Tool(*run, open) }})
}

// finishTool puts a result into the latest call with its id still running.
func (m *model) finishTool(id, output string, isError bool) {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		run := m.blocks[i].tool
		if run == nil || run.id != id || run.done {
			continue
		}
		m.blocks = slices.Clone(m.blocks)
		finished := *run
		finished.output, finished.isError, finished.done = output, isError, true
		printer := m.printer
		m.blocks[i].tool = &finished
		m.blocks[i].draw = func(open bool) string { return printer.Tool(finished, open) }
		m.blocks[i].text = m.blocks[i].draw(m.blocks[i].open)
		return
	}
}

// scheduleRedraw redraws the transcript once the resizing has stopped for
// a moment, not at every step of it.
func (m *model) scheduleRedraw() tea.Cmd {
	m.redraws++
	number := m.redraws
	return tea.Tick(redrawDelay, func(time.Time) tea.Msg { return redrawMsg(number) })
}

func (m *model) redraw() {
	m.blocks = slices.Clone(m.blocks)
	for i, b := range m.blocks {
		if b.draw != nil {
			m.blocks[i].text = b.draw(b.open)
		}
	}
}

// reasoningStyle is the grey of reasoning, darker than the faint of notes.
func (p *printer) reasoningStyle() lipgloss.Style {
	if p.isDark() {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#9699a3"))
}

// fold is a blank line and a grey header naming what is folded, then all of
// it when open; while a reasoning is being written, its last lines.
func (p *printer) fold(title, text string, open, live bool) string {
	style := p.reasoningStyle()
	p.mu.Lock()
	width := p.width
	p.mu.Unlock()
	wrap := lipgloss.NewStyle().Width(max(width-6, 20)).PaddingLeft(4)
	var header string
	switch {
	case live:
		header = "  ∴ thinking…"
	case open:
		header = "  ▾ " + title + " · ctrl+o or click to fold"
	default:
		header = fmt.Sprintf("  ▸ %s · %d lines · ctrl+o or click to open", title, strings.Count(text, "\n")+1)
	}
	shown := ""
	switch {
	case open:
		shown = wrap.Render(text)
	case live:
		all := strings.Split(wrap.Render(text), "\n")
		shown = strings.Join(all[max(len(all)-liveReasoningLines, 0):], "\n")
	}
	if shown == "" {
		return "\n" + style.Render(header)
	}
	return "\n" + style.Render(header+"\n"+shown)
}

// transcript is the text of every block, and the block each line of a
// foldable one belongs to, to open it with a click.
func (m model) transcript() (string, map[int]int) {
	var out strings.Builder
	foldable := map[int]int{}
	line := 0
	for i, b := range m.blocks {
		lines := strings.Count(b.text, "\n") + 1
		if b.foldable {
			for row := line; row < line+lines; row++ {
				foldable[row] = i
			}
		}
		out.WriteString(b.text)
		out.WriteByte('\n')
		line += lines
	}
	return out.String(), foldable
}

// toggleDetails opens or folds every reasoning and tool call, and the ones
// to come.
func (m *model) toggleDetails() {
	m.showDetails = !m.showDetails
	m.blocks = slices.Clone(m.blocks)
	for i, b := range m.blocks {
		if b.foldable {
			m.blocks[i].open = m.showDetails
			m.blocks[i].text = b.draw(m.showDetails)
		}
	}
}

// clickDetails opens or folds the block at row y of the screen, and tells
// whether there was one there.
func (m *model) clickDetails(y int) bool {
	conversation := m.conversation(m.conversationHeight())
	_, foldable := m.transcript()
	i, found := foldable[y+conversation.YOffset()]
	if !found {
		return false
	}
	m.blocks = slices.Clone(m.blocks)
	m.blocks[i].open = !m.blocks[i].open
	m.blocks[i].text = m.blocks[i].draw(m.blocks[i].open)
	return true
}

// replay draws a conversation again from its history, as after a resume or
// going back: prompts, reasonings, answers and tool calls with their
// results. mo's own notes to the model are left out.
func (m *model) replay(history []llm.Message) {
	m.blocks = nil
	m.todos = m.todosFrom(history)
	printer := m.printer
	for _, message := range history {
		switch {
		case agent.IsSummary(message):
			summary := agent.Summary(message)
			m.addFoldable(block{draw: func(open bool) string {
				return printer.fold("summary of the earlier conversation", summary, open, false)
			}})
		case message.Role == llm.RoleUser && agent.IsPrompt(message):
			prompt := agent.PromptText(message)
			m.blocks = append(m.blocks, block{text: printer.User(prompt), draw: func(bool) string { return printer.User(prompt) }})
		case message.Role == llm.RoleAssistant:
			m.addReasoning(message.Reasoning)
			if text := strings.TrimSpace(message.Text); text != "" {
				m.blocks = append(m.blocks, block{text: printer.Answer(text), draw: func(bool) string { return printer.Answer(text) }})
			}
			for _, call := range message.ToolCalls {
				call = m.unwrap(call)
				m.addTool(call, m.toolSummary(call))
			}
		case message.Role == llm.RoleTool:
			m.finishTool(message.ToolCallID, message.Text, message.IsError)
		}
	}
}

// unwrap shows a tool_call as the tool it ran, the way it was shown live.
func (m model) unwrap(call llm.ToolCall) llm.ToolCall {
	if m.session.Agent == nil || m.session.Agent.Tools == nil {
		return call
	}
	if inner, _, err := m.session.Agent.Tools.Unwrap(call); err == nil {
		return inner
	}
	return call
}

func (m model) toolSummary(call llm.ToolCall) string {
	if m.session.Agent == nil || m.session.Agent.Tools == nil {
		return string(call.Args)
	}
	return m.session.Agent.Tools.Summary(call)
}
