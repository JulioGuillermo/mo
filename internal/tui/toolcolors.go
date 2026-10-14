package tui

import (
	"encoding/json"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// toolKind is what a tool call does, shown by the colour of its mark: a
// command that blocks the model, one in the background, reading a
// command's output, changing something, or only reading.
type toolKind int

const (
	kindRead toolKind = iota
	kindWrite
	kindExec
	kindBackground
	kindOutput
)

var toolKindColors = map[toolKind]color.Color{
	kindRead:       lipgloss.Color("#7aa2f7"),
	kindWrite:      lipgloss.Color("#ff79c6"),
	kindExec:       lipgloss.Color("#f7768e"),
	kindBackground: lipgloss.Color("#e0af68"),
	kindOutput:     lipgloss.Color("#9ece6a"),
}

// toolMark is the coloured dot before a tool call.
func toolMark(kind toolKind) string {
	return lipgloss.NewStyle().Foreground(toolKindColors[kind]).Bold(true).Render("●")
}

// toolKindOf tells what a call does; changes says whether the registry
// counts it as a change, nil without one.
func toolKindOf(call llm.ToolCall, changes func(llm.ToolCall) bool) toolKind {
	if call.Name == "bash" {
		switch tools.BashAction(call.Args) {
		case "run":
			var fields struct {
				Background bool `json:"background"`
			}
			_ = json.Unmarshal(call.Args, &fields)
			if fields.Background {
				return kindBackground
			}
			return kindExec
		case "kill":
			return kindExec
		}
		return kindOutput
	}
	if alwaysChanging[call.Name] || (changes != nil && changes(call)) {
		return kindWrite
	}
	return kindRead
}

// toolKind is what a call does, as the session's registry sees it.
func (m model) toolKind(call llm.ToolCall) toolKind {
	var changes func(llm.ToolCall) bool
	if m.session.Agent != nil && m.session.Agent.Tools != nil {
		registry := m.session.Agent.Tools
		changes = func(call llm.ToolCall) bool { return registry.Changes(call) || registry.Warning(call) != "" }
	}
	return toolKindOf(m.unwrap(call), changes)
}
