package tui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

// fileChange is what a write or an edit did to a file, from its arguments:
// what a write wrote, or the pairs of old and new text of an edit, one for the
// old and new arguments, several for edits.
type fileChange struct {
	path, content string
	edits         []fileEdit
	edit          bool
}

// fileEdit is a pair of old and new text, and the file it is in.
type fileEdit struct{ path, old, new string }

func fileChangeOf(name string, args json.RawMessage) (fileChange, bool) {
	var fields struct {
		Path, Content, Old, New string
		Edits                   []struct{ Path, Old, New string }
	}
	if (name != "write" && name != "edit") || json.Unmarshal(args, &fields) != nil {
		return fileChange{}, false
	}
	change := fileChange{path: fields.Path, content: fields.Content, edit: name == "edit"}
	if change.edit {
		if fields.Old != "" || fields.New != "" {
			change.edits = append(change.edits, fileEdit{path: fields.Path, old: fields.Old, new: fields.New})
		}
		for _, each := range fields.Edits {
			change.edits = append(change.edits, fileEdit{path: cmp.Or(each.Path, fields.Path), old: each.Old, new: each.New})
		}
	}
	return change, change.content != "" || len(change.edits) > 0
}

// files are the files an edit changes, each once, in order.
func (c fileChange) files() []string {
	var files []string
	for _, each := range c.edits {
		if !slices.Contains(files, each.path) {
			files = append(files, each.path)
		}
	}
	return files
}

// summary counts the lines a change writes, adds and removes.
func (c fileChange) summary() string {
	if !c.edit {
		return fmt.Sprintf("%d lines", strings.Count(strings.TrimRight(c.content, "\n"), "\n")+1)
	}
	added, removed := 0, 0
	for _, each := range c.edits {
		for _, line := range strings.Split(gitops.Unified("", each.old, each.new), "\n") {
			switch {
			case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			case strings.HasPrefix(line, "+"):
				added++
			case strings.HasPrefix(line, "-"):
				removed++
			}
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
	// The cursor's line, lighter: a fold, a line put in, taken out, kept.
	blockCursor, addedCursor, removedCursor, cursor color.Color
}

func changePalette(dark bool) changeColors {
	c := ui(dark)
	return changeColors{
		block: col(c.block), added: col(c.added), removed: col(c.removed),
		addedMark: col(c.green), removedMark: col(c.red), hunk: col(c.faint),
		blockCursor: col(c.blockCursor), addedCursor: col(c.addedCursor), removedCursor: col(c.removedCursor),
		cursor: col(c.cursorLine),
	}
}

// fileChangeLines shows a change: what a write wrote, highlighted, or what
// an edit changed, as a diff whose lines are highlighted too.
func (p *printer) fileChangeLines(change fileChange) string {
	text, _ := p.fileChangeBlock(change)
	return text
}

// fileChangeBlock is a change as lines, and, for a long write, the function
// that paints the lines in view: they are plain in the text until then.
func (p *printer) fileChangeBlock(change fileChange) (string, func(from, to int) []string) {
	p.mu.Lock()
	width, dark := p.width, p.dark
	p.mu.Unlock()
	colors := changePalette(dark)
	language := languageOf(change.path)
	inner := max(width-8, 20)
	edge := commandEdge().Render("  ▌")
	if !change.edit {
		block := lipgloss.NewStyle().Background(colors.block)
		lead, fill := block.Render(" "), newSGR(block)
		code := newCodeText(change.content, language, inner)
		return codeBlock(code, colors.block, dark, func(_ int, piece string) string {
			return edge + lead + piece + fill.wrap(strings.Repeat(" ", max(inner+1-ansi.StringWidth(piece), 0)))
		})
	}
	var out []string
	files := change.files()
	for i, each := range change.edits {
		if len(files) > 1 && (i == 0 || each.path != change.edits[i-1].path) {
			out = append(out, edge+toolName().Render(" "+each.path))
		}
		out = append(out, p.diffRows(each, languageOf(each.path), inner, colors, dark)...)
	}
	return strings.Join(out, "\n"), nil
}

// hunkHeader is the line a diff starts a part of the text with, and the lines
// it begins at in the old text and the new.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// diffRows are the lines of an edit as a diff: those taken out on red, those
// put in on green, each coloured by what the old text or the new one makes of
// it as a whole, so what a string or a comment holds across lines is known.
func (p *printer) diffRows(each fileEdit, language string, inner int, colors changeColors, dark bool) []string {
	oldCode, newCode := newCodeText(each.old, language, inner-2), newCodeText(each.new, language, inner-2)
	edge := commandEdge().Render("  ▌")
	pad := func(line string, background color.Color) string {
		fill := lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", max(inner+2-ansi.StringWidth(line), 0)))
		return edge + line + fill
	}
	var out []string
	oldAt, newAt := 0, 0
	for _, line := range strings.Split(strings.TrimRight(gitops.Unified("", each.old, each.new), "\n"), "\n") {
		switch {
		case line == "", strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"), strings.HasPrefix(line, `\`):
			continue
		case strings.HasPrefix(line, "@@"):
			if found := hunkHeader.FindStringSubmatch(line); found != nil {
				oldAt, _ = strconv.Atoi(found[1])
				newAt, _ = strconv.Atoi(found[2])
				oldAt, newAt = oldAt-1, newAt-1
			}
			out = append(out, pad(lipgloss.NewStyle().Background(colors.block).Foreground(colors.hunk).Render(" "+line), colors.block))
			continue
		}
		code, at, background := newCode, newAt, colors.block
		mark := lipgloss.NewStyle().Background(background).Render("  ")
		switch line[0] {
		case '+':
			background, newAt = colors.added, newAt+1
			mark = lipgloss.NewStyle().Background(background).Foreground(colors.addedMark).Bold(true).Render("+ ")
		case '-':
			code, at, background, oldAt = oldCode, oldAt, colors.removed, oldAt+1
			mark = lipgloss.NewStyle().Background(background).Foreground(colors.removedMark).Bold(true).Render("- ")
		default:
			newAt, oldAt = newAt+1, oldAt+1
		}
		var spans [][2]int
		if at < len(code.lines) {
			spans = wrapSpans(code.lines[at], inner-2)
		} else {
			spans = [][2]int{{0, 0}}
		}
		for i, span := range spans {
			piece := ""
			if at < len(code.lines) {
				piece = code.paint(at, span[0], span[1], background, dark)
			}
			if i > 0 {
				mark = lipgloss.NewStyle().Background(background).Render("  ")
			}
			out = append(out, pad(mark+piece, background))
		}
	}
	return out
}
