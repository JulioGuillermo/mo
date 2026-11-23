package tui

import (
	"strings"

	"github.com/jg/mo/internal/render"
)

// visualsNote tells the model what of its answers this screen draws, for
// images in mode: images and svg blocks only where images are drawn, any
// mermaid diagram where mermaid-cli draws them as pictures.
func visualsNote(mode render.Mode, mermaidCLI bool) string {
	lines := []string{"Your answers are drawn in a terminal that can render:"}
	if mode != render.ModeOff {
		lines = append(lines,
			"- local images, as a line of its own: ![name](path) (PNG, JPEG, GIF, WebP, SVG)",
			"- svg code blocks")
	}
	if mermaidCLI && (mode == render.ModeKitty || mode == render.ModeSixel) {
		lines = append(lines, "- mermaid code blocks: any diagram")
	} else {
		lines = append(lines, "- mermaid code blocks: flowchart, sequence, ER and class diagrams")
	}
	lines = append(lines, "Use them when a picture explains better than text.")
	return strings.Join(lines, "\n")
}

// tellVisuals tells the agent what the screen draws, as the settings let it.
func (m model) tellVisuals() {
	if m.session.Agent == nil {
		return
	}
	if setting := m.session.Config.Render.TellModel; setting != nil && !*setting {
		m.session.Agent.SetVisuals("")
		return
	}
	mode := m.printer.renderer().Mode
	mermaidCLI := (mode == render.ModeKitty || mode == render.ModeSixel) && render.MermaidCLI()
	m.session.Agent.SetVisuals(visualsNote(mode, mermaidCLI))
}
