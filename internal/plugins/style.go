package plugins

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/tools"
)

// styleText is mo.style(text, { fg, bg, bold, italic, underline, reverse }):
// the text with those colours and marks, for a canvas to draw. Colours are
// #rrggbb or a name of the palette dialogs use.
func styleText(state *lua.LState) int {
	text := state.CheckString(1)
	options := state.OptTable(2, state.NewTable())
	style := lipgloss.NewStyle().
		Bold(lua.LVAsBool(options.RawGetString("bold"))).
		Italic(lua.LVAsBool(options.RawGetString("italic"))).
		Underline(lua.LVAsBool(options.RawGetString("underline"))).
		Reverse(lua.LVAsBool(options.RawGetString("reverse")))
	if fg := tools.UIColor(luaString(options.RawGetString("fg"))); fg != "" {
		style = style.Foreground(lipgloss.Color(fg))
	}
	if bg := tools.UIColor(luaString(options.RawGetString("bg"))); bg != "" {
		style = style.Background(lipgloss.Color(bg))
	}
	state.Push(lua.LString(style.Render(text)))
	return 1
}

// textWidth is mo.width(text): the cells text takes on screen, its colour
// codes left out and wide characters counted as two.
func textWidth(state *lua.LState) int {
	state.Push(lua.LNumber(ansi.StringWidth(state.CheckString(1))))
	return 1
}
