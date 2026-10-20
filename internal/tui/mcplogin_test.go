package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | ML-01 | Happy    | A server waiting for a sign-in shows it in /mcp, with the command        | P0       |
// | ML-02 | Happy    | /mcp login alone takes the only server waiting; a name not configured says so | P1   |
// | ML-03 | Happy    | Tab after /mcp login offers only the servers that sign in, not those with a key; logout only those signed in | P2 |
// | ML-04 | Edge     | /mcp logout of a server never signed in to says so                       | P2       |
// | ML-05 | Edge     | Saving a server in its form keeps the sign-in settings the config has    | P1       |
// | ML-06 | Happy    | The sign-in page shows whole, broken over lines, and is copied           | P1       |

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
)

func signInModel(t *testing.T) model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := conversationModel()
	m.session.Config.MCP = map[string]config.MCPServer{
		"jira":  {URL: "https://mcp.example.com/v2/mcp"},
		"keyed": {URL: "https://other.example.com/mcp", Headers: map[string]string{"authorization": "Bearer key"}},
		"local": {Command: "npx"},
	}
	m.session.MCP = []mcphub.Status{
		{Name: "jira", Err: fmt.Errorf("%w: /mcp login jira", mcphub.ErrNeedsLogin)},
		{Name: "local", Tools: 3},
	}
	return m
}

func TestMCPSignIn(t *testing.T) {
	t.Run("ML-01 a server waiting for a sign-in shows it in /mcp, with the command", func(t *testing.T) {
		m := signInModel(t)

		report := mcpReport(m.session.MCP, m.session.Config)

		if !strings.Contains(report, "! jira: needs a sign-in · /mcp login jira") || !strings.Contains(report, "✓ local: 3 tools") {
			t.Errorf("report:\n%s", report)
		}
		if state := m.mcpState("jira"); !strings.Contains(state, "needs a sign-in") {
			t.Errorf("state = %q", state)
		}
	})

	t.Run("ML-02 /mcp login alone takes the only server waiting; a name not configured says so", func(t *testing.T) {
		m := signInModel(t)

		waiting := m.onlyServerToSignIn()
		updated, _ := m.mcpSignIn("login", "nope")

		if waiting != "jira" || !strings.Contains(shown(updated.(model)), "no MCP server nope") {
			t.Errorf("waiting = %q shown:\n%s", waiting, shown(updated.(model)))
		}
	})

	t.Run("ML-03 tab after /mcp login offers only the servers that sign in, not those with a key; logout only those signed in", func(t *testing.T) {
		m := signInModel(t)

		actions := suggestionNames(m.mcpSignInSuggestions("lo"))
		servers := m.mcpSignInSuggestions("login ")
		signedOut := m.mcpSignInSuggestions("logout ")

		if strings.Join(actions, ",") != "mcp login ,mcp logout " || len(servers) != 1 || servers[0].name != "mcp login jira" || servers[0].description != "needs a sign-in" || len(signedOut) != 0 {
			t.Errorf("actions = %v servers = %v signedOut = %v", actions, servers, signedOut)
		}
	})

	t.Run("ML-04 /mcp logout of a server never signed in to says so", func(t *testing.T) {
		m := signInModel(t)

		updated, _ := m.mcpSignIn("logout", "jira")

		if !strings.Contains(shown(updated.(model)), "there was no sign-in to jira") {
			t.Errorf("shown:\n%s", shown(updated.(model)))
		}
	})

	t.Run("ML-05 saving a server in its form keeps the sign-in settings the config has", func(t *testing.T) {
		oauth := &config.OAuth{Scopes: []string{"read:jira"}}
		draft := newMCPDraft("jira", config.MCPServer{URL: "https://mcp.example.com/v2/mcp", OAuth: oauth})
		draft.trust = true

		layer, err := draft.applyTo(config.Config{})

		saved := layer.MCP["jira"]
		if err != nil || saved.OAuth == nil || saved.OAuth.Scopes[0] != "read:jira" || !saved.Trust {
			t.Errorf("saved = %+v err = %v", saved, err)
		}
	})

	t.Run("ML-06 the sign-in page shows whole, broken over lines, and is copied", func(t *testing.T) {
		m := signInModel(t)
		m.printer.setWidth(40)
		page := "https://auth.example.com/authorize?client_id=abc&scope=" + strings.Repeat("x", 80) + "&state=END"

		updated, cmd := m.showSignInPage(signInPageMsg{url: page, code: "WXYZ-1234"})
		m = updated.(model)

		text := strings.ReplaceAll(strings.ReplaceAll(shown(m), "\n", ""), " ", "")
		if !strings.Contains(text, strings.ReplaceAll(page, " ", "")) || !strings.Contains(shown(m), "enter the code WXYZ-1234") || m.flash != "sign-in page copied" || cmd == nil {
			t.Errorf("shown:\n%s\nflash %q", shown(m), m.flash)
		}
	})
}
