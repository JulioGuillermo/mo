package tui

import (
	"fmt"
	"image/color"
	"path"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
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
	case m.waitingFor != "":
		return state{word: "waiting", detail: m.waitingFor, tint: colors.waiting}
	case m.activity != "":
		if m.activity == agent.CompactingActivity {
			return state{word: "compacting", tint: colors.compacting}
		}
		return state{word: "checking", detail: m.activity, tint: colors.compacting}
	case m.formingNow() != nil:
		// A call being written: those that ran stay listed meanwhile.
		last := m.formingNow()
		return m.toolState(last.name, last.args, colors)
	case m.runningTool != nil:
		return m.toolState(m.runningTool.Name, string(m.runningTool.Args), colors)
	case m.lastEvent == llm.EventPromptProgress && m.promptRead.Total > 0:
		return state{word: "prompt", detail: promptDetail(m.promptRead), tint: colors.prompt}
	case m.lastEvent == llm.EventReasoning && m.reasoning != "":
		return state{word: "reasoning", tint: colors.reasoning}
	case m.lastEvent == llm.EventText && m.stream != "":
		return state{word: "talking", tint: colors.writing}
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
	tint, word := colors.reading, "reading"
	if changes {
		tint, word = colors.changing, "changing"
	}
	if verb, ok := toolVerbs[call.Name]; ok {
		word = verb
	}
	args = string(call.Args)
	field := func(name string) string {
		value, _ := partialString(args, name)
		return oneLine(value, stateDetailWidth)
	}
	detail := call.Name
	switch call.Name {
	case "read", "write", "edit", "ls", "mkdir", "rm":
		detail = orDefault(field("path"), call.Name)
	case "grep":
		detail = field("pattern")
	case "fetch":
		detail = field("url")
	case "web_search":
		detail = field("query")
	case "git":
		word, detail = "versioning", field("action")
	case tools.AgentName:
		detail = field("name")
	case "bash":
		command, _ := partialString(args, "command")
		detail = programs(command)
	}
	if server, tool, ok := mcpTool(call.Name); ok {
		word, detail = "calling", server+" › "+tool
	}
	return state{word: word, detail: detail, tint: tint}
}

// stateDetailWidth is how much of a call's detail the status line shows.
const stateDetailWidth = 40

// toolVerbs are what the status line says each tool does; the others read
// or change, as they do.
var toolVerbs = map[string]string{
	"read": "reading", "grep": "scanning", "ls": "listing", "write": "writing", "edit": "editing",
	"rm": "removing", "mkdir": "creating", "bash": "running", "fetch": "fetching", "web_search": "searching",
	"memory": "remembering", "todo": "planning", tools.AgentName: "delegating", "ui": "drawing",
}

// trivialPrograms say nothing of what a command does, beside others.
var trivialPrograms = map[string]bool{"cd": true, "echo": true, "export": true, "true": true, "false": true,
	"sleep": true, "printf": true, "set": true, ":": true, "pwd": true}

// programs are the programs a command runs, once each, without arguments:
// the first two that say what it does, and how many more.
func programs(command string) string {
	var names []string
	if commands, err := permissions.Commands(command); err == nil {
		for _, each := range commands {
			if fields := strings.Fields(each.Text); len(fields) > 0 {
				names = append(names, path.Base(fields[0]))
			}
		}
	} else if fields := strings.Fields(command); len(fields) > 0 {
		// Still being written: its first word.
		names = append(names, path.Base(fields[0]))
	}
	var shown []string
	for _, name := range names {
		if !trivialPrograms[name] && !slices.Contains(shown, name) {
			shown = append(shown, name)
		}
	}
	if len(shown) == 0 && len(names) > 0 {
		shown = names[:1]
	}
	switch {
	case len(shown) == 0:
		return "bash"
	case len(shown) > 2:
		return strings.Join(shown[:2], ", ") + fmt.Sprintf(" +%d", len(shown)-2)
	}
	return strings.Join(shown, ", ")
}

// mcpTool splits an MCP tool's name, mcp__server__tool.
func mcpTool(name string) (string, string, bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	server, tool, ok := strings.Cut(rest, "__")
	return server, tool, ok
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
