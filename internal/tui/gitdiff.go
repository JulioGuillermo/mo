package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

const (
	diffContext    = 3
	diffExpandStep = 20
)

// diffText is a file's diff as the viewer shows it: every line of the old
// text and the new, those far from a change folded into one row until
// they are shown, a few at a time, all, or the whole file at once.
type diffText struct {
	edits       []gitops.Line
	shown       []bool
	whole       bool
	rows        []diffRow
	before      *codeText
	after       *codeText
	numberWidth int
}

// diffRow is a line of the diff, the edit it shows, or a fold of hidden
// lines from that edit on.
type diffRow struct {
	edit   int
	hidden int
}

func (r diffRow) folded() bool {
	return r.hidden > 0
}

// newDiffText is the diff from before to after, nil when the text is the
// same.
func newDiffText(before, after, language string) *diffText {
	before, after = cleanCode(before), cleanCode(after)
	d := &diffText{edits: gitops.Lines(before, after), before: newCodeText(before, language, 1<<30), after: newCodeText(after, language, 1<<30)}
	if !d.changed() {
		return nil
	}
	d.numberWidth = len(strconv.Itoa(max(len(d.before.lines), len(d.after.lines))))
	d.shown = d.nearChanges()
	d.layout()
	return d
}

func (d *diffText) changed() bool {
	for _, edit := range d.edits {
		if edit.Kind != ' ' {
			return true
		}
	}
	return false
}

// nearChanges marks the lines changed and the ones diffContext around them.
func (d *diffText) nearChanges() []bool {
	shown := make([]bool, len(d.edits))
	last := -diffContext - 1
	for i, edit := range d.edits {
		if edit.Kind != ' ' {
			last = i
		}
		shown[i] = i-last <= diffContext
	}
	last = len(d.edits) + diffContext + 1
	for i := len(d.edits) - 1; i >= 0; i-- {
		if d.edits[i].Kind != ' ' {
			last = i
		}
		shown[i] = shown[i] || last-i <= diffContext
	}
	return shown
}

// layout lists the rows again from the lines shown.
func (d *diffText) layout() {
	d.rows = d.rows[:0]
	for i := 0; i < len(d.edits); {
		if d.whole || d.shown[i] {
			d.rows = append(d.rows, diffRow{edit: i})
			i++
			continue
		}
		end := i
		for end < len(d.edits) && !d.shown[end] {
			end++
		}
		d.rows = append(d.rows, diffRow{edit: i, hidden: end - i})
		i = end
	}
}

// expand shows lines of the fold at row: all of them, or diffExpandStep,
// taken next to the changes around it. It tells whether row was a fold.
func (d *diffText) expand(row int, all bool) bool {
	if row < 0 || row >= len(d.rows) || !d.rows[row].folded() {
		return false
	}
	from, count := d.rows[row].edit, d.rows[row].hidden
	to := from + count
	reveal := count
	if !all {
		reveal = min(count, diffExpandStep)
	}
	above, below := reveal/2, reveal-reveal/2
	switch {
	case from == 0:
		above, below = 0, reveal
	case to == len(d.edits):
		above, below = reveal, 0
	}
	for i := from; i < from+above; i++ {
		d.shown[i] = true
	}
	for i := to - below; i < to; i++ {
		d.shown[i] = true
	}
	d.layout()
	return true
}

// toggleWhole shows the whole file, or only the lines shown before.
func (d *diffText) toggleWhole() {
	d.whole = !d.whole
	d.layout()
}

// lines are the rows as plain text, to search and copy.
func (d *diffText) lines() []string {
	out := make([]string, len(d.rows))
	for i := range d.rows {
		out[i] = d.plain(i)
	}
	return out
}

func (d *diffText) plain(i int) string {
	row := d.rows[i]
	if row.folded() {
		return d.foldLabel(row)
	}
	edit := d.edits[row.edit]
	return fmt.Sprintf("%*d %c %s", d.numberWidth, d.number(edit), edit.Kind, strings.TrimRight(edit.Text, "\n"))
}

func (d *diffText) foldLabel(row diffRow) string {
	label := fmt.Sprintf("⋯ %d unchanged lines · e shows %d · E all · f the whole file", row.hidden, min(row.hidden, diffExpandStep))
	if row.hidden <= diffExpandStep {
		label = fmt.Sprintf("⋯ %d unchanged lines · e or E shows them · f the whole file", row.hidden)
	}
	return strings.Repeat(" ", d.numberWidth+1) + label
}

// number is a line's number in the text it is in: the old one for a line
// taken out, the new one otherwise.
func (d *diffText) number(edit gitops.Line) int {
	if edit.Kind == '-' {
		return edit.Old + 1
	}
	return edit.New + 1
}

// edit is the line a row shows; a fold shows none.
func (d *diffText) edit(row int) (int, bool) {
	if row < 0 || row >= len(d.rows) || d.rows[row].folded() {
		return 0, false
	}
	return d.rows[row].edit, true
}

// rowOf is the row an edit is on, or the fold that hides it.
func (d *diffText) rowOf(edit int) int {
	for i, row := range d.rows {
		if edit >= row.edit && edit < row.edit+max(row.hidden, 1) {
			return i
		}
	}
	return max(len(d.rows)-1, 0)
}

// paint is a row coloured: its number and mark, then the code, on the
// colour of what the line is, a lighter one under the cursor.
func (d *diffText) paint(i int, dark, cursor bool) string {
	row := d.rows[i]
	colors := changePalette(dark)
	background := d.background(i, dark, cursor)
	if row.folded() {
		return lipgloss.NewStyle().Background(background).Foreground(colors.hunk).Render(d.foldLabel(row))
	}
	edit := d.edits[row.edit]
	code, line, markColor := d.after, edit.New, colors.hunk
	switch edit.Kind {
	case '-':
		code, line, markColor = d.before, edit.Old, colors.removedMark
	case '+':
		markColor = colors.addedMark
	}
	gutter := lipgloss.NewStyle().Background(background).Foreground(markColor)
	text := ""
	if line < len(code.lines) {
		text = code.paint(line, 0, 1<<30, background, dark)
	}
	return gutter.Render(fmt.Sprintf("%*d %c ", d.numberWidth, line+1, edit.Kind)) + text
}

// background is the colour a row is drawn on, to the end of the screen:
// a fold's, a line put in's or taken out's, lighter under the cursor.
func (d *diffText) background(i int, dark, cursor bool) color.Color {
	colors := changePalette(dark)
	row := d.rows[i]
	kind := byte('@')
	if !row.folded() {
		kind = d.edits[row.edit].Kind
	}
	switch {
	case kind == '@' && cursor:
		return colors.blockCursor
	case kind == '@':
		return colors.block
	case kind == '+' && cursor:
		return colors.addedCursor
	case kind == '+':
		return colors.added
	case kind == '-' && cursor:
		return colors.removedCursor
	case kind == '-':
		return colors.removed
	case cursor:
		return colors.cursor
	}
	return lipgloss.NoColor{}
}

// fillRow paints the rest of a row, to width, on its colour.
func (d *diffText) fillRow(i int, line string, width int, dark, cursor bool) string {
	missing := width - ansi.StringWidth(line)
	if missing <= 0 {
		return line
	}
	return line + lipgloss.NewStyle().Background(d.background(i, dark, cursor)).Render(strings.Repeat(" ", missing))
}

// describe says where a line of the diff is, for the model.
func (d *diffText) describe(edit int) string {
	line := d.edits[edit]
	switch line.Kind {
	case '+':
		return fmt.Sprintf("line %d, added", line.New+1)
	case '-':
		return fmt.Sprintf("old line %d, removed", line.Old+1)
	}
	return fmt.Sprintf("line %d", line.New+1)
}

// commentNote is how the model gets the comments on a diff: each with the
// lines it is on, the first of them quoted.
func (d *diffText) commentNote(file, title string, comments []fileComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The user's comments on the changes to %s (%s):", file, title)
	for _, comment := range comments {
		where := d.describe(comment.from)
		if comment.to > comment.from {
			where = "from " + where + " to " + d.describe(comment.to)
		}
		fmt.Fprintf(&b, "\n- %s (`%s`): %s", where, quote(d.edits[comment.from].Text), comment.text)
	}
	return b.String()
}

// expandDiff shows the lines of the fold under the cursor: a few, or all.
func (m model) expandDiff(all bool) {
	v := m.viewer
	if v.diff == nil {
		return
	}
	if !v.diff.expand(v.cursor, all) {
		v.notice = "e and E show the lines a ⋯ row hides: move onto one"
		return
	}
	m.diffChanged()
}

// toggleWholeDiff shows the whole file, or the changes alone again, the
// cursor kept on its line.
func (m model) toggleWholeDiff() {
	v := m.viewer
	if v.diff == nil {
		return
	}
	edit := v.diff.rows[min(v.cursor, len(v.diff.rows)-1)].edit
	v.diff.toggleWhole()
	v.cursor = v.diff.rowOf(edit)
	m.diffChanged()
	v.top = max(v.cursor-m.viewerRows()/2, 0)
}

// diffChanged takes the rows of the diff after they changed.
func (m model) diffChanged() {
	v := m.viewer
	v.lines = v.diff.lines()
	v.selected, v.selecting = false, false
	v.findMatches()
	m.moveCursor(0)
}
