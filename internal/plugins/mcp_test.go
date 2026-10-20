package plugins

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | PM-01 | Happy    | mo.mcp declares a server by url or command, with its settings, marked as the plugin's | P0 |
// | PM-02 | Happy    | MCPHeaders runs the plugin's function for a request; a table of headers needs none | P0 |
// | PM-03 | Edge     | A server whose name is taken is left out with one warning, however often asked | P1 |
// | PM-04 | Error    | A server with both or neither of url and command, a function's headers for a command, or a name twice fails the load | P1 |
// | PM-05 | Error    | A header function that fails gives its error                               | P1       |

import (
	"context"
	"strings"
	"testing"
)

const mcpPlugin = `
mo.setting{ name = "token", default = "t0" }
mo.mcp{ name = "board", url = "https://board.example/mcp", ca = "~/ca.pem", trust = true,
  headers = function(request)
    return { Authorization = "Bearer " .. mo.settings.get("token"), ["X-Method"] = request.method .. " " .. request.url }
  end }
mo.mcp{ name = "local", command = "board-mcp", args = { "--stdio" }, env = { LEVEL = "debug" } }
mo.mcp{ name = "fixed", url = "https://fixed.example/mcp", headers = { ["X-Key"] = "k" } }`

func TestPluginMCP(t *testing.T) {
	t.Run("PM-01 mo.mcp declares a server by url or command, with its settings, marked as the plugin's", func(t *testing.T) {
		set := loadSource(t, map[string]string{"jira": mcpPlugin}).set

		servers := set.MCPServers(nil)

		board, local, fixed := servers["board"], servers["local"], servers["fixed"]
		if len(servers) != 3 || board.URL != "https://board.example/mcp" || board.CA != "~/ca.pem" || !board.Trust || board.Plugin != "jira" {
			t.Errorf("board = %+v", board)
		}
		if local.Command != "board-mcp" || strings.Join(local.Args, " ") != "--stdio" || local.Env["LEVEL"] != "debug" || local.Plugin != "jira" {
			t.Errorf("local = %+v", local)
		}
		if fixed.Headers["X-Key"] != "k" {
			t.Errorf("fixed = %+v", fixed)
		}
	})

	t.Run("PM-02 MCPHeaders runs the plugin's function for a request; a table of headers needs none", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{"jira": mcpPlugin}, map[string]map[string]any{"jira": {"token": "t1"}})

		headers, err := set.MCPHeaders(context.Background(), "jira", "board", "POST", "https://board.example/mcp")
		fixed, fixedErr := set.MCPHeaders(context.Background(), "jira", "fixed", "POST", "https://fixed.example/mcp")

		if err != nil || headers["Authorization"] != "Bearer t1" || headers["X-Method"] != "POST https://board.example/mcp" {
			t.Errorf("headers = %v err = %v", headers, err)
		}
		if fixed != nil || fixedErr != nil {
			t.Errorf("fixed = %v err = %v", fixed, fixedErr)
		}
	})

	t.Run("PM-03 a server whose name is taken is left out with one warning, however often asked", func(t *testing.T) {
		set := loadSource(t, map[string]string{
			"a": `mo.mcp{ name = "board", url = "https://a.example/mcp" }`,
			"b": `mo.mcp{ name = "board", url = "https://b.example/mcp" } mo.mcp{ name = "mine", url = "https://b.example/mine" }`,
		}).set

		set.MCPServers([]string{"mine"})
		servers := set.MCPServers([]string{"mine"})

		warnings := set.Status()[1].Warnings
		if len(servers) != 1 || servers["board"].URL != "https://a.example/mcp" || len(warnings) != 2 {
			t.Errorf("servers = %v warnings = %q", servers, warnings)
		}
	})

	t.Run("PM-04 a server with both or neither of url and command, a function's headers for a command, or a name twice fails the load", func(t *testing.T) {
		set := loadSource(t, map[string]string{
			"both":    `mo.mcp{ name = "x", url = "https://x", command = "x" }`,
			"neither": `mo.mcp{ name = "x" }`,
			"headers": `mo.mcp{ name = "x", command = "x", headers = function() return {} end }`,
			"twice":   `mo.mcp{ name = "x", url = "https://x" } mo.mcp{ name = "x", url = "https://y" }`,
			"late":    `mo.command{ name = "late", run = function() mo.mcp{ name = "x", url = "https://x" } end }`,
		}).set

		_, lateErr := runCommand(t, set, "late")

		for _, status := range set.Status() {
			if status.Name != "late" && status.Err == nil {
				t.Errorf("plugin %s loaded", status.Name)
			}
		}
		if lateErr == nil {
			t.Error("mo.mcp worked after loading")
		}
	})

	t.Run("PM-05 a header function that fails gives its error", func(t *testing.T) {
		set := loadSource(t, map[string]string{"jira": `mo.mcp{ name = "board", url = "https://x", headers = function() error("no token yet", 0) end }`}).set

		_, err := set.MCPHeaders(context.Background(), "jira", "board", "POST", "https://x")

		if err == nil || !strings.Contains(err.Error(), "headers of board") || !strings.Contains(err.Error(), "no token yet") {
			t.Errorf("err = %v", err)
		}
	})
}
