package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | TP-01 | Happy    | mo's tools are rows of their own, each MCP server one row with how much is ticked | P0 |
// | TP-02 | Happy    | Space on a server ticks it whole as mcp__server__*, again unticks it     | P0       |
// | TP-03 | Happy    | Enter on a server opens its tools; some ticked are kept by name, all ticked become its pattern | P0 |
// | TP-04 | Happy    | Space ticks a tool of mo; done keeps the list, esc puts the old one back | P0       |
// | TP-05 | Happy    | Disabled, Ask before running, Loaded from the start and a type's Tools open the pick | P1 |
// | TP-06 | Edge     | A server not connected ticks whole on enter; a tool no longer there stays ticked | P1 |

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
)

func pickModel(chosen *[]string) (model, *toolPickScreen) {
	m, _ := menuModel()
	names := []string{"read", "bash", "mcp__xdr__search", "mcp__xdr__list", "mcp__xdr__get"}
	pick := newToolPick(names, []string{"osdx", "xdr"}, chosen)
	screen := &toolPickScreen{title: "Disabled", pick: pick, cancel: func(m model) (tea.Model, tea.Cmd) { return m, nil }}
	screen.keep = screen.cancel
	shown, _ := m.showToolPick(screen, "")
	return shown.(model), screen
}

func pressEnter(m model) model {
	updated, cmd := m.Update(keyEnter)
	return deliver(updated.(model), cmd, 3)
}

func shownRows(m model) string {
	return ansi.Strip(m.settings.form.View())
}

func TestToolPick(t *testing.T) {
	t.Run("TP-01 mo's tools are rows of their own, each MCP server one row with how much is ticked", func(t *testing.T) {
		chosen := []string{"bash", "mcp__xdr__get"}
		m, _ := pickModel(&chosen)

		view := shownRows(m)

		for _, want := range []string{"· read", "✓ bash", "◐ xdr", "1/3", "· osdx", "not connected"} {
			if !strings.Contains(view, want) {
				t.Errorf("no %q in\n%s", want, view)
			}
		}
		if strings.Contains(view, "search") {
			t.Errorf("a server's tool is a row of its own:\n%s", view)
		}
	})

	t.Run("TP-02 space on a server ticks it whole as mcp__server__*, again unticks it", func(t *testing.T) {
		chosen := []string{"mcp__xdr__get"}
		m, _ := pickModel(&chosen)

		m = keys(t, m, keyDown, keyDown, keyDown, keySpace)
		whole := slices.Clone(chosen)
		m = keys(t, m, keySpace)

		if !slices.Equal(whole, []string{"mcp__xdr__*"}) || len(chosen) != 0 {
			t.Errorf("after one space %v, after two %v", whole, chosen)
		}
	})

	t.Run("TP-03 enter on a server opens its tools; some ticked are kept by name, all ticked become its pattern", func(t *testing.T) {
		chosen := []string{}
		m, screen := pickModel(&chosen)
		xdr := screen.pick.server("xdr")

		m = pressEnter(keys(t, m, keyDown, keyDown, keyDown))
		opened := strings.Contains(shownRows(m), "Disabled › xdr")
		m = pressEnter(keys(t, m, keySpace))
		some := slices.Clone(chosen)
		screen.pick.setServer(xdr, xdr.tools)

		if !opened || !slices.Equal(some, []string{"mcp__xdr__search"}) || !slices.Equal(chosen, []string{"mcp__xdr__*"}) {
			t.Errorf("opened %v, some %v, all %v", opened, some, chosen)
		}
	})

	t.Run("TP-04 space ticks a tool of mo; done keeps the list, esc puts the old one back", func(t *testing.T) {
		m, menu := menuModel()
		section, _ := sectionByKey("tools")
		opened, _ := m.editItem(menu, section, itemNamed(m, menu, section, disabledTitle))

		ticked := keys(t, opened.(model), keySpace)
		kept := slices.Clone(menu.draft.disabled)
		ticked = keys(t, ticked, keySpace, keyEsc)

		if len(kept) != 1 || len(menu.draft.disabled) != 0 || ticked.settings == nil {
			t.Errorf("kept %v, after esc %v", kept, menu.draft.disabled)
		}
	})

	t.Run("TP-05 Disabled, Ask before running, Loaded from the start and a type's Tools open the pick", func(t *testing.T) {
		m, menu := menuModel()
		section, _ := sectionByKey("tools")
		for _, title := range []string{disabledTitle, confirmTitle, alwaysLoadedTitle} {
			opened, _ := m.editItem(menu, section, itemNamed(m, menu, section, title))
			if screen := opened.(model).settings; screen == nil || screen.toggle == nil || screen.help != toolPickHelp {
				t.Errorf("%s does not open the pick", title)
			}
		}
		draft := newAgentTypeDraft("reviewer", config.AgentType{})
		back := func(m model) (tea.Model, tea.Cmd) { return m, nil }
		opened, _ := m.pickTypeTools(menu, draft, "Type reviewer", back)
		if screen := opened.(model).settings; screen == nil || screen.toggle == nil {
			t.Errorf("a type's Tools do not open the pick")
		}
	})

	t.Run("TP-06 a server not connected ticks whole on enter; a tool no longer there stays ticked", func(t *testing.T) {
		chosen := []string{"old_tool"}
		m, _ := pickModel(&chosen)

		m = pressEnter(keys(t, m, keyDown, keyDown))

		if !slices.Equal(chosen, []string{"old_tool", "mcp__osdx__*"}) || !strings.Contains(shownRows(m), "✓ osdx") {
			t.Errorf("chosen %v\n%s", chosen, shownRows(m))
		}
	})
}
