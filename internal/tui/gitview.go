package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

const (
	gitUsage   = "usage: /git show, the changes not committed · /git compare <branch>, what this branch adds over it, as a pull request"
	changeHelp = "↑↓ scroll · ←→ long lines · / search · n N next · y copy · tab tree · t hide it · esc close"
)

var (
	changeAdded    = lipgloss.Color("#9ece6a")
	changeDeleted  = lipgloss.Color("#f7768e")
	changeModified = warning
)

// changeView is a comparison shown in the viewer: its files in the tree,
// the diff of the one picked beside it.
type changeView struct {
	title      string
	root       string
	comparison gitops.Comparison
	tree       *fileTree
}

// diffText is a file's diff as the viewer paints it: each row a line of
// the old text or the new, or a hunk's header, painted when in view.
type diffText struct {
	rows        []diffRow
	before      *codeText
	after       *codeText
	numberWidth int
}

type diffRow struct {
	mark   byte
	line   int
	header string
}

// changeStyle is the colour of a file by what happened to it: new in
// green, changed in amber, gone in red; bold for the one shown.
func changeStyle(kind rune, current bool) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(changeModified)
	switch kind {
	case gitops.ChangeAdded, gitops.ChangeUntracked:
		style = style.Foreground(changeAdded)
	case gitops.ChangeDeleted:
		style = style.Foreground(changeDeleted)
	}
	return style.Bold(current)
}

// gitCommand is /git show and /git compare <branch>.
func (m model) gitCommand(argument string) (tea.Model, tea.Cmd) {
	action, branch, _ := strings.Cut(strings.TrimSpace(argument), " ")
	branch = strings.TrimSpace(branch)
	repo, err := gitops.Open(m.session.WorkDir)
	if err != nil {
		return m.emitError(err)
	}
	var comparison gitops.Comparison
	var title, nothing string
	switch {
	case action == "show" && branch == "":
		comparison, err = repo.Uncommitted()
		title, nothing = "Changes not committed", "nothing changed since the last commit"
	case action == "compare" && branch != "":
		comparison, err = repo.Compare(branch)
		title = branch + " → " + orDefault(repo.CurrentBranch(), "HEAD")
		nothing = "this branch adds nothing over " + branch
	default:
		return m.emit(m.printer.Note(gitUsage))
	}
	if err != nil {
		return m.emitError(err)
	}
	if len(comparison.Files) == 0 {
		return m.emit(m.printer.Note(nothing))
	}
	changes := &changeView{title: title, root: repo.Root(), comparison: comparison, tree: newChangeTree(repo.Root(), comparison.Files)}
	first := filepath.Join(changes.root, comparison.Files[0].Path)
	m.openChange(changes, first)
	changes.tree.reveal(first, m.treeHeight())
	m.viewer.tree, m.viewer.treeFocus = changes.tree, true
	if m.width < treeNarrow {
		m.viewer.tree, m.viewer.treeFocus = nil, false
	}
	return m, nil
}

// newChangeTree lists only the files of a comparison, every folder open.
func newChangeTree(root string, files []gitops.Change) *fileTree {
	tree := &fileTree{root: filepath.Clean(root), expanded: map[string]bool{}, kinds: map[string]rune{}}
	folders := map[string][]string{}
	names := map[string][]string{}
	for _, file := range files {
		full := filepath.Join(tree.root, file.Path)
		tree.kinds[full] = file.Kind
		tree.indexed = append(tree.indexed, file.Path)
		names[filepath.Dir(full)] = append(names[filepath.Dir(full)], filepath.Base(full))
		for dir := filepath.Dir(full); dir != tree.root && !tree.expanded[dir]; dir = filepath.Dir(dir) {
			tree.expanded[dir] = true
			parent := filepath.Dir(dir)
			folders[parent] = append(folders[parent], filepath.Base(dir))
		}
	}
	tree.listing = func(dir string) ([]string, []string) {
		return sortedNames(folders[dir]), sortedNames(names[dir])
	}
	tree.rebuild()
	return tree
}

// sortedNames are names in the order the tree lists them, capitals not
// mattering.
func sortedNames(names []string) []string {
	sorted := slices.Clone(names)
	slices.SortFunc(sorted, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return sorted
}

// openChange shows the diff of one file of a comparison in the viewer.
func (m *model) openChange(changes *changeView, path string) {
	relative, _ := filepath.Rel(changes.root, path)
	before, after := changes.comparison.Contents(filepath.ToSlash(relative))
	v := &outputViewer{changes: changes, diffFile: relative, pickFrom: -1}
	if isBinary(before) || isBinary(after) {
		v.lines = []string{"a binary file: its changes are not shown"}
	} else {
		v.diff, v.lines = newDiffText(before, after, languageOf(path))
		if len(v.lines) == 0 {
			v.lines = []string{"no changes in the text"}
			v.diff = nil
		}
	}
	changes.tree.current = path
	m.viewer = v
}

func isBinary(text string) bool {
	return strings.IndexByte(text[:min(len(text), 8000)], 0) >= 0
}

// newDiffText is the diff from before to after, three lines around each
// change, and its lines as plain text, to search and copy.
func newDiffText(before, after, language string) (*diffText, []string) {
	d := &diffText{before: newCodeText(before, language, 1<<30), after: newCodeText(after, language, 1<<30)}
	d.numberWidth = len(strconv.Itoa(max(len(d.before.lines), len(d.after.lines))))
	var plain []string
	beforeAt, afterAt := 0, 0
	for _, line := range strings.Split(gitops.Unified("", cleanCode(before), cleanCode(after)), "\n") {
		switch {
		case line == "", strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"), strings.HasPrefix(line, `\`):
			continue
		case strings.HasPrefix(line, "@@"):
			if found := hunkHeader.FindStringSubmatch(line); found != nil {
				beforeAt, _ = strconv.Atoi(found[1])
				afterAt, _ = strconv.Atoi(found[2])
				beforeAt, afterAt = max(beforeAt-1, 0), max(afterAt-1, 0)
			}
			d.rows = append(d.rows, diffRow{mark: '@', header: line})
			plain = append(plain, line)
			continue
		}
		row := diffRow{mark: line[0], line: afterAt}
		switch line[0] {
		case '-':
			row.line = beforeAt
			beforeAt++
		case '+':
			afterAt++
		default:
			beforeAt++
			afterAt++
		}
		d.rows = append(d.rows, row)
		plain = append(plain, fmt.Sprintf("%*d %c %s", d.numberWidth, row.line+1, row.mark, line[1:]))
	}
	return d, plain
}

// paint is a row of the diff coloured: its number and mark, then the code,
// on the colour of what the row is.
func (d *diffText) paint(i int, dark bool) string {
	row := d.rows[i]
	colors := changePalette(dark)
	background := d.background(i, dark)
	if row.mark == '@' {
		return lipgloss.NewStyle().Background(background).Foreground(colors.hunk).Render(row.header)
	}
	code, markColor := d.after, colors.hunk
	switch row.mark {
	case '-':
		code, markColor = d.before, colors.removedMark
	case '+':
		markColor = colors.addedMark
	}
	gutter := lipgloss.NewStyle().Background(background).Foreground(markColor)
	text := ""
	if row.line < len(code.lines) {
		text = code.paint(row.line, 0, 1<<30, background, dark)
	}
	return gutter.Render(fmt.Sprintf("%*d %c ", d.numberWidth, row.line+1, row.mark)) + text
}

// background is the colour a row is drawn on, to the end of the screen.
func (d *diffText) background(i int, dark bool) color.Color {
	colors := changePalette(dark)
	switch d.rows[i].mark {
	case '+':
		return colors.added
	case '-':
		return colors.removed
	case '@':
		return colors.block
	}
	return lipgloss.NoColor{}
}

// fillRow paints the rest of a diff row, to width, on its colour.
func (d *diffText) fillRow(i int, line string, width int, dark bool) string {
	missing := width - ansi.StringWidth(line)
	if missing <= 0 {
		return line
	}
	return line + lipgloss.NewStyle().Background(d.background(i, dark)).Render(strings.Repeat(" ", missing))
}

// changeHeader is what the viewer says over a file of a comparison.
func (v *outputViewer) changeHeader(width int) string {
	about := fmt.Sprintf("%s · %d files", v.changes.title, len(v.changes.comparison.Files))
	return toolName.Render(oneLine(v.diffFile, width/2)) + " " + dimStyle.Render(about+" · only you see it")
}

// gitSuggestions are what /git takes, then the branches /git compare does,
// those that start with what was typed.
func (m model) gitSuggestions(partial string) []command {
	if branch, comparing := strings.CutPrefix(partial, "compare "); comparing {
		repo, err := gitops.Open(m.session.WorkDir)
		if err != nil {
			return nil
		}
		var matches []command
		for _, name := range repo.BranchNames() {
			if strings.HasPrefix(name, branch) && name != repo.CurrentBranch() {
				matches = append(matches, command{name: "git compare " + name, description: "what this branch adds over " + name})
			}
		}
		return matches
	}
	actions := []command{
		{name: "git show", description: "the changes not committed, staged or not"},
		{name: "git compare ", description: "what this branch adds over another, as a pull request", more: true},
	}
	var matches []command
	for _, action := range actions {
		if strings.HasPrefix(action.name, "git "+partial) {
			matches = append(matches, action)
		}
	}
	return matches
}
