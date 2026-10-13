package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

// fileChange is what a write or an edit did to a file, from its arguments.
type fileChange struct {
	path, content, old, new string
	edit                    bool
}

func fileChangeOf(name string, args json.RawMessage) (fileChange, bool) {
	var fields struct {
		Path, Content, Old, New string
	}
	if (name != "write" && name != "edit") || json.Unmarshal(args, &fields) != nil {
		return fileChange{}, false
	}
	change := fileChange{path: fields.Path, content: fields.Content, old: fields.Old, new: fields.New, edit: name == "edit"}
	return change, change.content != "" || change.old != "" || change.new != ""
}

// summary counts the lines a change writes, adds and removes.
func (c fileChange) summary() string {
	if !c.edit {
		return fmt.Sprintf("%d lines", strings.Count(strings.TrimRight(c.content, "\n"), "\n")+1)
	}
	added, removed := 0, 0
	for _, line := range strings.Split(gitops.Unified("", c.old, c.new), "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return fmt.Sprintf("+%d −%d", added, removed)
}

// languageOf names the chroma lexer for a file, or none.
func languageOf(path string) string {
	if lexer := lexers.Match(filepath.Base(path)); lexer != nil {
		return lexer.Config().Name
	}
	return ""
}

type changeColors struct {
	block, added, removed, addedMark, removedMark, hunk color.Color
}

func changePalette(dark bool) changeColors {
	if dark {
		return changeColors{
			block: lipgloss.Color("#0e1630"), added: lipgloss.Color("#12301f"), removed: lipgloss.Color("#3a1622"),
			addedMark: lipgloss.Color("#9ece6a"), removedMark: lipgloss.Color("#f7768e"), hunk: lipgloss.Color("#565f89"),
		}
	}
	return changeColors{
		block: lipgloss.Color("#e6ecfa"), added: lipgloss.Color("#dcf2dc"), removed: lipgloss.Color("#f8dde2"),
		addedMark: lipgloss.Color("#587539"), removedMark: lipgloss.Color("#f52a65"), hunk: lipgloss.Color("#8990b3"),
	}
}

// fileChangeLines shows a change: what a write wrote, highlighted, or what
// an edit changed, as a diff whose lines are highlighted too.
func (p *printer) fileChangeLines(change fileChange) string {
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	colors := changePalette(dark)
	language := languageOf(change.path)
	inner := max(width-8, 20)
	var out []string
	pad := func(line string, background color.Color) string {
		fill := lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", max(inner+2-ansi.StringWidth(line), 0)))
		return commandEdge.Render("  ▌") + line + fill
	}
	if !change.edit {
		for _, line := range highlightLines(change.content, language, inner, colors.block, dark) {
			out = append(out, pad(lipgloss.NewStyle().Background(colors.block).Render(" ")+line, colors.block))
		}
		return strings.Join(out, "\n")
	}
	for _, line := range strings.Split(strings.TrimRight(gitops.Unified("", change.old, change.new), "\n"), "\n") {
		switch {
		case line == "", strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"), strings.HasPrefix(line, `\`):
			continue
		case strings.HasPrefix(line, "@@"):
			out = append(out, pad(lipgloss.NewStyle().Background(colors.block).Foreground(colors.hunk).Render(" "+line), colors.block))
			continue
		}
		background, mark := colors.block, lipgloss.NewStyle().Background(colors.block).Render("  ")
		switch line[0] {
		case '+':
			background = colors.added
			mark = lipgloss.NewStyle().Background(background).Foreground(colors.addedMark).Bold(true).Render("+ ")
		case '-':
			background = colors.removed
			mark = lipgloss.NewStyle().Background(background).Foreground(colors.removedMark).Bold(true).Render("- ")
		}
		for i, piece := range highlightLines(line[1:], language, inner-2, background, dark) {
			if i > 0 {
				mark = lipgloss.NewStyle().Background(background).Render("  ")
			}
			out = append(out, pad(mark+piece, background))
		}
	}
	return strings.Join(out, "\n")
}
