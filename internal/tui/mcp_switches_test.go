package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | MS-01 | Happy    | /mcp's editor shows whether the server is on and its mode; the menu's does not | P0  |
// | MS-02 | Happy    | Turned off and set to group there, the user file holds both; on again, neither | P0 |
// | MS-03 | Edge     | A project that sets the servers off or the mode gets the change too       | P1       |
// | MS-04 | Happy    | /mcp lists each server with how it is doing                               | P1       |
// | MS-05 | Happy    | Esc closes the suggestions and leaves the model working; typing opens them again | P0 |
// | MS-06 | Happy    | A server not in full mode shows its description switch and length; one in full does not | P0 |
// | MS-07 | Happy    | The description switched off and its length go to the file; a bad length is refused | P0 |

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/tools"
)

func mcpModel(t *testing.T) (model, settingsLoadedMsg) {
	t.Helper()
	m := conversationModel()
	file := config.Config{MCP: map[string]config.MCPServer{"docs": {URL: "http://x/mcp"}, "web": {Command: "npx"}}}
	file.Tools.MCPDisabled = []string{"web"}
	m.session.Config.MCP = file.MCP
	m.session.Config.Tools.MCPDisabled = []string{"web"}
	m.session.Config.Tools.MCPModes = map[string]tools.Mode{"docs": tools.ModeCompact}
	m.session.MCP = []mcphub.Status{{Name: "docs", Tools: 4}}
	return m, settingsLoadedMsg{kind: mcpSettings, layer: file}
}

func TestMCPSwitches(t *testing.T) {
	t.Run("MS-01 /mcp's editor shows whether the server is on and its mode; the menu's does not", func(t *testing.T) {
		m, msg := mcpModel(t)
		fromMCP, _ := m.editMCPServer(msg, "docs", nil)
		fromMenu, _ := m.editMCPServer(msg, "docs", &settingsMenu{})
		off, _ := m.editMCPServer(msg, "web", nil)

		if view := screen(fromMCP.(model)); !strings.Contains(view, "On ") || !strings.Contains(view, "compact") {
			t.Errorf("from /mcp:\n%s", view)
		}
		if view := screen(off.(model)); !strings.Contains(view, "off") {
			t.Errorf("off server:\n%s", view)
		}
		if view := screen(fromMenu.(model)); strings.Contains(view, "Tool mode") {
			t.Errorf("from the menu:\n%s", view)
		}
	})

	t.Run("MS-02 turned off and set to group there, the user file holds both; on again, neither", func(t *testing.T) {
		draft := newMCPDraft("docs", config.MCPServer{URL: "http://x/mcp"})
		draft.switches, draft.on, draft.mode = true, false, string(tools.ModeGroup)
		layer := config.Config{}
		layer.Tools.MCPDisabled = []string{"web"}

		off, err := draft.applyTo(layer)
		draft.on, draft.mode = true, ""
		on, _ := draft.applyTo(off)

		if err != nil || !slices.Equal(off.Tools.MCPDisabled, []string{"docs", "web"}) || off.Tools.MCPModes["docs"] != tools.ModeGroup {
			t.Errorf("off: %v %v %v", err, off.Tools.MCPDisabled, off.Tools.MCPModes)
		}
		if !slices.Equal(on.Tools.MCPDisabled, []string{"web"}) || len(on.Tools.MCPModes) != 0 {
			t.Errorf("on: %v %v", on.Tools.MCPDisabled, on.Tools.MCPModes)
		}
		if !slices.Equal(layer.Tools.MCPDisabled, []string{"web"}) {
			t.Errorf("the layer given changed: %v", layer.Tools.MCPDisabled)
		}
	})

	t.Run("MS-03 a project that sets the servers off or the mode gets the change too", func(t *testing.T) {
		draft := newMCPDraft("docs", config.MCPServer{URL: "http://x/mcp"})
		draft.switches, draft.on, draft.mode = true, false, string(tools.ModeSearch)
		setting := config.Config{}
		setting.Tools.MCPDisabled = []string{}
		quiet := config.Config{Provider: "x"}

		changed := draft.projectSwitches(settingsLoadedMsg{project: config.Project{Layer: setting, Trusted: true}})
		untouched := draft.projectSwitches(settingsLoadedMsg{project: config.Project{Layer: quiet, Trusted: true}})
		untrusted := draft.projectSwitches(settingsLoadedMsg{project: config.Project{Layer: setting}})

		if changed == nil || !slices.Equal(changed.Tools.MCPDisabled, []string{"docs"}) || changed.Tools.MCPModes != nil {
			t.Errorf("changed = %+v", changed)
		}
		if untouched != nil || untrusted != nil {
			t.Errorf("untouched = %+v untrusted = %+v", untouched, untrusted)
		}
	})

	t.Run("MS-04 /mcp lists each server with how it is doing", func(t *testing.T) {
		m, msg := mcpModel(t)
		msg.layer.MCP = maps.Clone(msg.layer.MCP)
		msg.layer.MCP["broken"] = config.MCPServer{Command: "nope"}
		m.session.MCP = append(m.session.MCP, mcphub.Status{Name: "broken", Err: errors.New("exec: not found")})

		picked, _ := m.pickMCPServer(msg, nil)
		view := screen(picked.(model))

		for _, want := range []string{"● 4 tools · compact", "○ off", "✗ exec: not found"} {
			if !strings.Contains(view, want) {
				t.Errorf("%q not in:\n%s", want, view)
			}
		}
	})

	t.Run("MS-05 esc closes the suggestions and leaves the model working; typing opens them again", func(t *testing.T) {
		m := conversationModel()
		stopped := false
		m.running, m.cancel = true, func() { stopped = true }
		m.input.SetValue("/he")
		open := len(m.suggestions()) > 0

		m = keys(t, m, keyEsc)
		closed := len(m.suggestions()) == 0
		m = typeIn(t, m, "l")
		again := len(m.suggestions()) > 0

		if !open || !closed || !again || stopped || !m.running {
			t.Errorf("open %v closed %v again %v stopped %v", open, closed, again, stopped)
		}
		m = keys(t, m, keyEsc, keyEsc, keyEsc)
		if !stopped {
			t.Error("esc with the suggestions closed does not stop the model")
		}
	})

	t.Run("MS-06 a server not in full mode shows its description switch and length; one in full does not", func(t *testing.T) {
		m, msg := mcpModel(t)
		compact, _ := m.editMCPServer(msg, "docs", &settingsMenu{})
		full, _ := m.editMCPServer(msg, "web", &settingsMenu{})

		if view := screen(compact.(model)); !strings.Contains(view, "Server description") || !strings.Contains(view, "2048 (default)") {
			t.Errorf("compact:\n%s", view)
		}
		if view := screen(full.(model)); strings.Contains(view, "Server description") {
			t.Errorf("full:\n%s", view)
		}
	})

	t.Run("MS-07 the description switched off and its length go to the file; a bad length is refused", func(t *testing.T) {
		draft := newMCPDraft("docs", config.MCPServer{URL: "http://x/mcp"})
		draft.describeChars = "500"
		kept, keptErr := draft.applyTo(config.Config{})
		draft.describe = false
		off, _ := draft.applyTo(config.Config{})
		draft.describeChars = "many"
		_, badErr := draft.applyTo(config.Config{})

		if server := kept.MCP["docs"]; keptErr != nil || server.Description != nil || server.DescriptionChars != 500 {
			t.Errorf("kept = %+v err = %v", server, keptErr)
		}
		if server := off.MCP["docs"]; server.Description == nil || *server.Description || server.DescriptionLimit() != 0 {
			t.Errorf("off = %+v", server)
		}
		if badErr == nil || !strings.Contains(badErr.Error(), "description length") {
			t.Errorf("bad length err = %v", badErr)
		}
	})
}
