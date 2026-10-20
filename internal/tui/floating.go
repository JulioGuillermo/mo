package tui

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	floatingMaxWidth = 76
	// floatingInset is the border and padding before a box's content.
	floatingInset = 2
)

// floatingSize is the room inside a box over a screen of that size.
func floatingSize(width, height int) (int, int) {
	return max(min(width-8, floatingMaxWidth), 20), max(height-6, settingsMinHeight)
}

// floatingContent is what the box holds: the form, and its help under it.
// The form fills the height it is given, so the rows it leaves empty are
// dropped, and an error shows right under the field.
func (m model) floatingContent() string {
	return withoutEmptyRuns(strings.TrimRight(m.settings.form.View(), "\n ")) + "\n\n" + dimStyle.Render(m.settings.helpLine())
}

// withoutEmptyRuns keeps one empty row where text has several in a row.
func withoutEmptyRuns(text string) string {
	var kept []string
	empty := false
	for _, line := range strings.Split(text, "\n") {
		blank := strings.TrimSpace(ansi.Strip(line)) == ""
		if blank && empty {
			continue
		}
		kept = append(kept, line)
		empty = blank
	}
	return strings.Join(kept, "\n")
}

// floatingBox is where the box goes, in the middle of the screen, as a
// region: its columns and its first row.
func (m model) floatingBox() region {
	lines := strings.Split(m.floatingFrame(), "\n")
	width := ansi.StringWidth(lines[0])
	x, y := max((m.width-width)/2, 0), max((m.height-len(lines))/2, 0)
	return region{y: y, x0: x, x1: x + width}
}

func (m model) floatingFrame() string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(popupBorder).Padding(0, 1).
		Render(m.floatingContent())
}

// floatingView is the screen a field was opened from with the field in a
// box over its middle.
func (m model) floatingView() string {
	box := m.floatingBox()
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(m.settings.under),
		lipgloss.NewLayer(m.floatingFrame()).X(box.x0).Y(box.y).Z(1),
	).Render()
}

// floats tells whether a field opens in a box: the short ones, a choice,
// yes or no, a line; long text keeps the whole screen.
func floats(field huh.Field) bool {
	_, long := field.(*huh.Text)
	return !long
}
