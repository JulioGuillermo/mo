package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// signInPageMsg is the page a sign-in waits on, and the code to enter
// there with the device flow.
type signInPageMsg struct {
	url, code string
}

// showSignInPage prints the whole address, broken over lines to fit, and
// copies it, since the browser may not open on its own.
func (m model) showSignInPage(msg signInPageMsg) (tea.Model, tea.Cmd) {
	width := max(m.printer.width-4, 20)
	lines := []string{"open this page to sign in (copied):"}
	lines = append(lines, strings.Split(ansi.Hardwrap(msg.url, width, false), "\n")...)
	if msg.code != "" {
		lines = append(lines, "and enter the code "+msg.code)
	}
	updated, shown := m.emit(m.printer.Note(strings.Join(lines, "\n")))
	m = updated.(model)
	m.flashes++
	m.flash = "sign-in page copied"
	number := m.flashes
	return m, tea.Batch(shown, tea.SetClipboard(msg.url), copyToSystem(msg.url),
		tea.Tick(flashTime, func(time.Time) tea.Msg { return flashDoneMsg(number) }))
}
