package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const sixelAttribute = 4

// imagesFor is how images are drawn for a setting: auto takes sixel in a
// terminal that answered it draws them and has no kitty graphics.
func imagesFor(setting render.Mode, sixelTerminal bool) render.Mode {
	mode := render.Detect(setting)
	auto := setting == render.ModeAuto || setting == ""
	if auto && sixelTerminal && mode != render.ModeKitty {
		return render.ModeSixel
	}
	return mode
}

// askTerminal asks the terminal what it can draw, when auto has no kitty
// graphics, and the size of its cells, when sixel is set.
func (m model) askTerminal() tea.Cmd {
	setting := m.session.Config.Render.Images
	switch {
	case render.Detect(setting) == render.ModeSixel:
		return tea.Raw(ansi.WindowOp(ansi.RequestCellSizeWinOp))
	case imagesFor(setting, true) == render.ModeSixel:
		return tea.Raw(ansi.RequestPrimaryDeviceAttributes)
	}
	return nil
}

// terminalAnswered takes what the terminal can do: one that draws sixel
// gets its images so, after it tells the size of its cells.
func (m model) terminalAnswered(attributes uv.PrimaryDeviceAttributesEvent) (tea.Model, tea.Cmd) {
	if !slices.Contains(attributes, sixelAttribute) {
		return m, nil
	}
	m.sixelTerminal = true
	m.printer.setImages(imagesFor(m.session.Config.Render.Images, true))
	return m, tea.Batch(tea.Raw(ansi.WindowOp(ansi.RequestCellSizeWinOp)), m.fitWidth())
}

func (m model) cellSizeAnswered(size uv.CellSizeEvent) (tea.Model, tea.Cmd) {
	render.SetReportedCellSize(size.Width, size.Height)
	if !m.printer.showsSixel() {
		return m, nil
	}
	return m, m.fitWidth()
}
