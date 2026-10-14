package tui

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// state is what the agent is doing now, in a word, for the spinner and the
// status line: reasoning, writing its answer, running a tool that reads or
// one that changes something, compacting, or waiting.
type state struct {
	word, detail string
	tint         color.Color
}

type stateColors struct {
	reasoning, writing, reading, changing, compacting, waiting, yours, prompt color.Color
}

func statePalette(dark bool) stateColors {
	if dark {
		return stateColors{
			reasoning: accent, writing: warning, reading: lipgloss.Color("#9ece6a"), changing: lipgloss.Color("#f7768e"),
			compacting: lipgloss.Color("#bb9af7"), waiting: lipgloss.Color("#737aa2"), yours: lipgloss.Color("#7dcfff"),
			prompt: lipgloss.Color("#73daca"),
		}
	}
	return stateColors{
		reasoning: lipgloss.Color("#2e7de9"), writing: lipgloss.Color("#8c6c3e"), reading: lipgloss.Color("#587539"), changing: lipgloss.Color("#f52a65"),
		compacting: lipgloss.Color("#9854f1"), waiting: lipgloss.Color("#8990b3"), yours: lipgloss.Color("#007197"),
		prompt: lipgloss.Color("#118c74"),
	}
}

// currentState works out what the agent is doing from what it last sent.
func (m model) currentState() state {
	colors := statePalette(m.printer == nil || m.printer.isDark())
	switch {
	case m.dialog != nil:
		return state{word: "your turn", detail: "answer above", tint: colors.yours}
	case m.activity != "":
		if m.activity == agent.CompactingActivity {
			return state{word: "compacting", tint: colors.compacting}
		}
		return state{word: "checking", detail: m.activity, tint: colors.compacting}
	case len(m.forming) > 0 && m.forming[len(m.forming)-1].name != "":
		last := m.forming[len(m.forming)-1]
		return m.toolState(last.name, last.args, colors)
	case m.runningTool != nil:
		return m.toolState(m.runningTool.Name, string(m.runningTool.Args), colors)
	case m.lastEvent == llm.EventPromptProgress && m.promptRead.Total > 0:
		return state{word: "prompt", detail: promptDetail(m.promptRead), tint: colors.prompt}
	case m.lastEvent == llm.EventReasoning && m.reasoning != "":
		return state{word: "reasoning", tint: colors.reasoning}
	case m.lastEvent == llm.EventText && m.stream != "":
		return state{word: "writing", tint: colors.writing}
	}
	return state{word: "waiting", detail: "for the model", tint: colors.waiting}
}

// toolState says whether a tool reads or changes something: changing is
// red, as are bash and anything the registry counts as a change.
func (m model) toolState(name, args string, colors stateColors) state {
	call := llm.ToolCall{Name: name, Args: []byte(args)}
	call = m.unwrap(call)
	changes := alwaysChanging[call.Name] || (call.Name == "bash" && tools.BashChanges(call.Args))
	if m.session.Agent != nil && m.session.Agent.Tools != nil {
		changes = changes || m.session.Agent.Tools.Changes(call) || m.session.Agent.Tools.Warning(call) != ""
	}
	if changes {
		return state{word: "changing", detail: call.Name, tint: colors.changing}
	}
	return state{word: "reading", detail: call.Name, tint: colors.reading}
}

// stateLabel is the word in its colour, and the detail after it.
func (s state) label(withDetail bool) string {
	text := lipgloss.NewStyle().Foreground(s.tint).Bold(true).Render(s.word)
	if withDetail && s.detail != "" {
		text += lipgloss.NewStyle().Foreground(s.tint).Render(" · " + s.detail)
	}
	return text
}

// alwaysChanging are the built-in tools that change something whatever
// their arguments; others the registry decides, as git and bash by their
// action.
var alwaysChanging = map[string]bool{"write": true, "edit": true, "mkdir": true, "rm": true}

// promptDetail is how far the prompt is read, leaving out the part the
// server had cached: "42% · 3.1k/7.4k", and how much came cached.
func promptDetail(read llm.PromptProgress) string {
	detail := fmt.Sprintf("%d%% · %s/%s", int(read.Share()*100), formatTokens(max(0, read.Done-read.Cached)), formatTokens(read.Total-read.Cached))
	if read.Cached > 0 {
		detail += " · " + formatTokens(read.Cached) + " cached"
	}
	return detail
}
