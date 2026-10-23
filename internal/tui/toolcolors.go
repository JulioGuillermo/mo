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

// toolKindColor is the colour of the mark of a kind of call.
func toolKindColor(kind toolKind) color.Color {
	colors := uiNow()
	switch kind {
	case kindWrite:
		return col(colors.pink)
	case kindExec:
		return col(colors.red)
	case kindBackground:
		return col(colors.warning)
	case kindOutput:
		return col(colors.green)
	}
	return col(colors.accent)
}

// toolMark is the coloured dot before a tool call.
func toolMark(kind toolKind) string {
	return toolMarkAs(kind, markDone)
}

// toolMarkAs is a call's sign of how it stands, coloured by what it does.
func toolMarkAs(kind toolKind, sign string) string {
	return lipgloss.NewStyle().Foreground(toolKindColor(kind)).Bold(true).Render(sign)
}

// toolKindOf tells what a call does; changes says whether the registry
// counts it as a change, nil without one.
func toolKindOf(call llm.ToolCall, changes func(llm.ToolCall) bool) toolKind {
	if tools.IsShell(call.Name) {
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
