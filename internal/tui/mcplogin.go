package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/mcphub"
)

const mcpSignInUsage = "/mcp login <server> signs in to a server reached by URL; /mcp logout <server> forgets it"

// mcpSignedInMsg is the end of a sign-in to an MCP server.
type mcpSignedInMsg struct {
	server string
	err    error
}

// mcpSignIn is /mcp login and /mcp logout: the server the user names, or
// the only one waiting for a sign-in.
func (m model) mcpSignIn(action, server string) (tea.Model, tea.Cmd) {
	if server == "" {
		server = m.onlyServerToSignIn()
	}
	settings, ok := m.session.Config.MCP[server]
	switch {
	case server == "":
		return m.emit(m.printer.Note(mcpSignInUsage))
	case !ok:
		return m.emit(m.printer.Note("no MCP server " + server + " · /mcp lists them"))
	case action == "logout":
		had, err := mcphub.Logout(server)
		if err != nil {
			return m.emitError(err)
		}
		if !had {
			return m.emit(m.printer.Note("there was no sign-in to " + server))
		}
		return m.reconnectMCP("signed out of " + server)
	}
	bridge := m.bridge
	show := func(prompt auth.Prompt) {
		if bridge != nil {
			bridge.send(signInPageMsg{url: prompt.URL, code: prompt.Code})
		}
	}
	note := printText(m.printer.Note("signing in to " + server + " · the browser opens…"))
	return m, tea.Batch(note, func() tea.Msg {
		return mcpSignedInMsg{server: server, err: mcphub.Login(context.Background(), server, settings, show)}
	})
}

// onlyServerToSignIn is the one server waiting for a sign-in, "" when there
// are none or several.
func (m model) onlyServerToSignIn() string {
	var waiting []string
	for _, status := range m.session.MCP {
		if mcphub.NeedsLogin(status.Err) {
			waiting = append(waiting, status.Name)
		}
	}
	if len(waiting) != 1 {
		return ""
	}
	return waiting[0]
}

func (m model) mcpSignedIn(msg mcpSignedInMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(fmt.Errorf("signing in to %s: %w", msg.server, msg.err))
	}
	return m.reconnectMCP("signed in to " + msg.server + " · its token is renewed when it expires; connecting…")
}

// reconnectMCP builds the session again on the settings in force, so the
// servers that failed connect again with what changed.
func (m model) reconnectMCP(note string) (tea.Model, tea.Cmd) {
	m.settingsInFlight = true
	return m, tea.Batch(printText(m.printer.Note(note)), rebuildSession(m.session, m.session.Config, false))
}

// mcpSignInSuggestions are the servers /mcp login and logout take.
func (m model) mcpSignInSuggestions(partial string) []command {
	action, typed, spaced := strings.Cut(partial, " ")
	if !spaced {
		var matches []command
		for _, each := range []command{
			{name: "mcp login ", description: "sign in to a server reached by URL", more: true},
			{name: "mcp logout ", description: "forget the sign-in to a server", more: true},
		} {
			if strings.HasPrefix(each.name, "mcp "+action) {
				matches = append(matches, each)
			}
		}
		return matches
	}
	if action != "login" && action != "logout" {
		return nil
	}
	var matches []command
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name, server := range m.session.Config.MCP {
			if server.URL != "" && !yield(name) {
				return
			}
		}
	}) {
		if !strings.HasPrefix(name, typed) {
			continue
		}
		description := "reached by URL"
		for _, status := range m.session.MCP {
			if status.Name == name && mcphub.NeedsLogin(status.Err) {
				description = "needs a sign-in"
			}
		}
		matches = append(matches, command{name: "mcp " + action + " " + name, description: description})
	}
	return matches
}
