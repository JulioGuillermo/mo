package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// region is a part of a drawn block that does something when clicked: a
// row, or the columns from x0 up to x1 of it.
type region struct {
	y, x0, x1 int
	do        func(m model) (tea.Model, tea.Cmd)
	// menu, when set, is what a right click there opens.
	menu func(m model) (tea.Model, tea.Cmd)
	// item names what the region is part of, a task or a job taking rows.
	item string
}

// withMenu gives regions the menu a right click opens.
func withMenu(regions []region, menu func(m model) (tea.Model, tea.Cmd)) []region {
	for i := range regions {
		regions[i].menu = menu
	}
	return regions
}

func row(y int, do func(m model) (tea.Model, tea.Cmd)) region {
	return region{y: y, x0: 0, x1: 1 << 30, do: do}
}

func (r region) holds(x, y int) bool {
	return y == r.y && x >= r.x0 && x < r.x1
}

// shifted moves regions down by lines, for a block drawn under others.
func shifted(regions []region, lines int) []region {
	moved := make([]region, len(regions))
	for i, r := range regions {
		r.y += lines
		moved[i] = r
	}
	return moved
}

// click does what is under the pointer: something in the bottom rows, a job
// in the side panel, or opening a folded block of the conversation.
func (m model) click(x, y int) (tea.Model, tea.Cmd) {
	_, regions := m.screenLayout()
	for _, r := range regions {
		if r.holds(x, y) {
			return r.do(m)
		}
	}
	if y < m.conversationHeight() && x < m.mainWidth() && m.dialog == nil {
		if drawn, ok := m.drawingAt(y); ok && !drawn.code && drawn.widest > m.printer.width {
			return m.openDrawing(drawn)
		}
		m.clickDetails(y)
	}
	return m, nil
}

// drawingMenu is what a right click on a diagram offers: its code in place
// of its drawing or back, seeing it whole, copying its code.
func (m model) drawingMenu(drawn drawing, path string) (tea.Model, tea.Cmd) {
	if drawn.picture != "" {
		return m.pictureMenu(drawn, path)
	}
	toggle := huh.NewOption("show its code", "code")
	if drawn.code {
		toggle = huh.NewOption("show the drawing", "code")
	}
	all := huh.NewOption("show all as code", "all")
	if drawn.code {
		all = huh.NewOption("draw all", "all")
	}
	options := []huh.Option[string]{toggle, all, huh.NewOption("see it whole", "whole"), huh.NewOption("copy its code", "copy")}
	options = append(options, fileOptions(path)...)
	return m.showChoices("Diagram", path, options, func(m model, choice string) (tea.Model, tea.Cmd) {
		if done, next, cmd := m.fileChoice(path, choice); done {
			return next, cmd
		}
		switch choice {
		case "code":
			m.printer.toggleCode(drawn.source)
			m.redraw()
			return m, nil
		case "all":
			m.printer.setAllCode(!drawn.code)
			m.redraw()
			return m, nil
		case "whole":
			return m.openDrawing(drawn)
		}
		return m.copyCode(drawn.source)
	})
}

// pictureMenu is what a right click on an image drawn from code offers:
// its code in its place, or the picture again, and copying its code.
func (m model) pictureMenu(drawn drawing, path string) (tea.Model, tea.Cmd) {
	toggle := huh.NewOption("show its code", "code")
	if drawn.code {
		toggle = huh.NewOption("show the picture", "code")
	}
	options := []huh.Option[string]{toggle, huh.NewOption("copy its code", "copy")}
	options = append(options, fileOptions(path)...)
	return m.showChoices("Picture", path, options, func(m model, choice string) (tea.Model, tea.Cmd) {
		if done, next, cmd := m.fileChoice(path, choice); done {
			return next, cmd
		}
		if choice == "copy" {
			return m.copyCode(drawn.source)
		}
		from, to := drawn.picture, drawn.codeText
		if drawn.code {
			from, to = to, from
		}
		m.printer.toggleCode(drawn.source)
		m.blocks = slices.Clone(m.blocks)
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if strings.Contains(m.blocks[i].text, from) {
				m.blocks[i].text = strings.Replace(m.blocks[i].text, from, to, 1)
				break
			}
		}
		return m, nil
	})
}

// copyCode copies a drawing's code and says so.
func (m model) copyCode(source string) (tea.Model, tea.Cmd) {
	source = strings.TrimSpace(source)
	m.flashes++
	m.flash = fmt.Sprintf("copied its code, %d lines", strings.Count(source, "\n")+1)
	number := m.flashes
	return m, tea.Batch(tea.SetClipboard(source), copyToSystem(source),
		tea.Tick(flashTime, func(time.Time) tea.Msg { return flashDoneMsg(number) }))
}

// drawingAt is the diagram drawn at row y of the conversation.
func (m model) drawingAt(y int) (drawing, bool) {
	row := y + m.conversationOffset()
	lines := m.conversationLines().window(row, row+1)
	if row < 0 || len(lines) == 0 {
		return drawing{}, false
	}
	return m.printer.drawingWith(ansi.Strip(lines[0]))
}

// rightClick opens the menu of what is under the pointer, if it has one.
func (m model) rightClick(x, y int) (tea.Model, tea.Cmd) {
	if m.dialog != nil || m.settings != nil {
		return m, nil
	}
	m.clickedAt, m.clicked = point{x, y}, true
	_, regions := m.screenLayout()
	for _, r := range regions {
		if r.menu != nil && r.holds(x, y) {
			return r.menu(m)
		}
	}
	if y < m.conversationHeight() && x < m.mainWidth() {
		if id, running := m.runningCommandAt(y); running {
			return m.jobMenu(id)
		}
		path, isFile := m.fileUnder(x, y)
		if drawn, ok := m.drawingAt(y); ok {
			return m.drawingMenu(drawn, path)
		}
		if isFile {
			return m.fileMenu(path)
		}
	}
	return m, nil
}

// clickSettings picks the option of a settings list that was clicked: it
// moves huh's cursor there and presses enter, as huh takes no mouse.
func (m model) clickSettings(x, y int) (tea.Model, tea.Cmd) {
	lines := strings.Split(ansi.Strip(m.settings.form.View()), "\n")
	if m.settings.tab != nil && y >= 0 && y < len(lines) && strings.Contains(lines[y], "tab switches") {
		screen := m.settings
		m.settings = nil
		return screen.tab(m)
	}
	if _, isConfirm := m.settings.form.GetFocusedField().(*huh.Confirm); isConfirm && y >= 0 && y < len(lines) {
		return m.clickButton(lines[y], x)
	}
	if _, isList := m.settings.form.GetFocusedField().(*huh.Select[string]); !isList {
		return m, nil
	}
	if y < 0 || y >= len(lines) || !isOptionLine(lines[y]) {
		return m, nil
	}
	moved, ok := m.moveListCursor(lines, y)
	if !ok {
		return m, nil
	}
	if moved.settings.remove != nil && onRemoveMark(lines[y], x) {
		screen := moved.settings
		moved.settings = nil
		return screen.remove(moved)
	}
	return moved.updateSettings(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// moveListCursor moves the cursor of a settings list to row y with the
// arrow keys, as huh takes no mouse.
func (m model) moveListCursor(lines []string, y int) (model, bool) {
	cursor := -1
	for i, line := range lines {
		if isOptionLine(line) && strings.HasPrefix(strings.TrimLeft(line, "┃│ "), ">") {
			cursor = i
		}
	}
	if cursor < 0 {
		return m, false
	}
	key := tea.KeyPressMsg{Code: tea.KeyDown}
	if y < cursor {
		key = tea.KeyPressMsg{Code: tea.KeyUp}
	}
	var current tea.Model = m
	for range abs(y - cursor) {
		current, _ = current.(model).updateSettings(key)
	}
	return current.(model), true
}

// onRemoveMark tells whether x falls on the delete mark of a list line.
func onRemoveMark(line string, x int) bool {
	at := strings.Index(line, removeMark)
	if at < 0 {
		return false
	}
	column := ansi.StringWidth(line[:at])
	return x >= column-1 && x <= column+1
}

// clickButton presses the button of a yes/no question under the pointer:
// the first accepts and the second rejects, as y and n do. Buttons are
// words, single spaces allowed, apart from others by two or more.
func (m model) clickButton(line string, x int) (tea.Model, tea.Cmd) {
	line = strings.NewReplacer("┃", " ", "│", " ").Replace(line)
	for index, at := range buttonText.FindAllStringIndex(line, 2) {
		left, right := ansi.StringWidth(line[:at[0]]), ansi.StringWidth(line[:at[1]])
		if x < left-2 || x >= right+2 {
			continue
		}
		key := "y"
		if index == 1 {
			key = "n"
		}
		return m.updateSettings(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	}
	return m, nil
}

var buttonText = regexp.MustCompile(`\S+(?: \S+)*`)

// isOptionLine tells a list option from a title or a description: options
// start after the border with the cursor or with blank room for it.
func isOptionLine(line string) bool {
	rest, bordered := strings.CutPrefix(line, "┃")
	if !bordered {
		rest, bordered = strings.CutPrefix(line, "│")
	}
	if !bordered {
		return false
	}
	return strings.HasPrefix(rest, " > ") || (strings.HasPrefix(rest, "   ") && strings.TrimSpace(rest) != "")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
