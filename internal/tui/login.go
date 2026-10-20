package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
)

// loginMsg is the end of a sign-in.
type loginMsg struct {
	provider string
	err      error
}

// login signs in to a provider that uses OAuth, the one in use by default:
// the page to open, and the code with the device flow, show as notes.
func (m model) login(argument string) (tea.Model, tea.Cmd) {
	provider := strings.TrimSpace(orDefault(argument, m.session.Provider))
	settings, ok := m.session.Config.Providers[provider]
	switch {
	case !ok:
		return m.emit(m.printer.Note("no provider " + provider + " · /providers lists them"))
	case settings.OAuth == nil:
		return m.emit(m.printer.Note(provider + " takes a key, not a sign-in · set [providers." + provider + ".oauth] to sign in"))
	}
	return m.loginTo(provider, *settings.OAuth)
}

// loginTo signs in to a provider with the given settings, which may be a
// form's not saved yet.
func (m model) loginTo(provider string, oauth config.OAuth) (tea.Model, tea.Cmd) {
	bridge := m.bridge
	show := func(prompt auth.Prompt) {
		if bridge != nil {
			bridge.send(signInPageMsg{url: prompt.URL, code: prompt.Code})
		}
	}
	note := printText(m.printer.Note("signing in to " + provider + "…"))
	return m, tea.Batch(note, func() tea.Msg {
		return loginMsg{provider: provider, err: auth.Login(context.Background(), provider, oauth, show)}
	})
}

func (m model) loggedIn(msg loginMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(fmt.Errorf("signing in to %s: %w", msg.provider, msg.err))
	}
	return m.emit(m.printer.Note("signed in to " + msg.provider + " · its token is renewed when it expires"))
}

// logout forgets a provider's token.
func (m model) logout(argument string) (tea.Model, tea.Cmd) {
	provider := strings.TrimSpace(orDefault(argument, m.session.Provider))
	if err := auth.Logout(provider); err != nil {
		return m.emitError(err)
	}
	return m.emit(m.printer.Note("signed out of " + provider))
}
