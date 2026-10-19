package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/peers"
)

// peerColors are the shades of the messages between peers: one background,
// a bar of its own colour for each way.
type peerColors struct {
	background, text, incoming, outgoing, status color.Color
}

func peerPalette(dark bool) peerColors {
	if dark {
		return peerColors{background: lipgloss.Color("#1e2030"), text: lipgloss.Color("#c0caf5"),
			incoming: lipgloss.Color("#bb9af7"), outgoing: lipgloss.Color("#7dcfff"), status: lipgloss.Color("#737aa2")}
	}
	return peerColors{background: lipgloss.Color("#e9e9ed"), text: lipgloss.Color("#3760bf"),
		incoming: lipgloss.Color("#9854f1"), outgoing: lipgloss.Color("#007197"), status: lipgloss.Color("#8990b3")}
}

// PeerIn draws a message a peer sent: its bar, who sent it, from which
// conversation and when, then the text; status says what became of it.
func (p *printer) PeerIn(from string, message peers.Message, status string) string {
	header := "⇠ " + from
	if message.From.Title != "" {
		header += fmt.Sprintf(" · %q", message.From.Title)
	}
	return p.peerBlock(true, header, message.At, status, message.Text)
}

// PluginIn draws a message a plugin sends the model.
func (p *printer) PluginIn(plugin, title, text, status string) string {
	return p.peerBlock(true, "⇠ plugin "+plugin+" · "+title, time.Now(), status, text)
}

// PeerOut draws a message this agent sent to a peer.
func (p *printer) PeerOut(label string, to peers.Info, text string) string {
	header := "⇢ to " + label
	if to.Title != "" {
		header += fmt.Sprintf(" · %q", to.Title)
	}
	return p.peerBlock(false, header, time.Now(), "", text)
}

func (p *printer) peerBlock(incoming bool, header string, at time.Time, status, text string) string {
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	colors := peerPalette(dark)
	bar := colors.outgoing
	if incoming {
		bar = colors.incoming
	}
	inner := max(width, 24) - 3
	shade := lipgloss.NewStyle().Background(colors.background)
	edge := lipgloss.NewStyle().Foreground(bar).Render("▌") + shade.Render(" ")
	row := func(text string) string {
		return edge + text + shade.Render(strings.Repeat(" ", max(inner-ansi.StringWidth(text), 0)))
	}
	about := " · " + at.Format("15:04")
	if status != "" {
		about += " · " + status
	}
	title := shade.Foreground(bar).Bold(true).Render(ansi.Truncate(header, inner-ansi.StringWidth(about), "…")) +
		shade.Foreground(colors.status).Render(about)
	lines := []string{row(title)}
	body := shade.Foreground(colors.text)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		for _, piece := range strings.Split(ansi.Wrap(line, inner, " "), "\n") {
			lines = append(lines, row(body.Render(piece)))
		}
	}
	return "\n" + strings.Join(lines, "\n")
}
