package tui

import (
	"container/list"
	"crypto/sha1"
	"image/color"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
)

// Code is coloured by its language in pieces, and only the pieces the screen
// needs. Recognising the syntax is what costs, about 1.4 ms for each KB, so:
//
//   - a text is cut into chunks at places a string or a comment is unlikely to
//     be open, and each chunk is recognised by itself;
//   - what a chunk recognised is kept by its content, so drawing again, at
//     another width or another background, costs nothing;
//   - only the rows in view are painted, which is the other half of the cost.
//
// So a file of any size costs what is on the screen.

const (
	// chunkTarget is how many lines a chunk has at least before it may end at a
	// safe place, and chunkMax the most it has wherever it ends.
	chunkTarget = 150
	chunkMax    = 600
	// tailChunkTarget and tailChunkMax are the same for the tail of a text
	// being written, which is recognised again at each line, so its chunks are
	// small: only the last grows.
	tailChunkTarget = 40
	tailChunkMax    = 120
	// tailContext is how many lines before a window are recognised with it so
	// a string or a comment that began above is known; tailMinContext is the
	// least, so a safe place to begin is looked for from there up.
	tailContext    = 250
	tailMinContext = 40
	// tokenCacheBytes is how much source text has its recognised pieces kept.
	tokenCacheBytes = 2 << 20
)

// codeToken is a piece of a line and what it is.
type codeToken struct {
	text string
	kind chroma.TokenType
}

type tokenKey struct {
	language string
	lines    int
	sum      [sha1.Size]byte
}

type tokenEntry struct {
	key  tokenKey
	rows [][]codeToken
	size int
}

// tokenCache keeps the pieces recognised in the texts last used, to the byte
// budget.
type tokenCache struct {
	mu      sync.Mutex
	entries map[tokenKey]*list.Element
	order   *list.List
	size    int
}

var tokensKept = &tokenCache{entries: map[tokenKey]*list.Element{}, order: list.New()}

// recognised counts the texts the lexers went through, for tests: what the
// cache holds does not add to it.
var recognised atomic.Int64

func (c *tokenCache) get(key tokenKey) ([][]codeToken, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, found := c.entries[key]
	if !found {
		return nil, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*tokenEntry).rows, true
}

func (c *tokenCache) put(key tokenKey, rows [][]codeToken, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, found := c.entries[key]; found {
		return
	}
	c.entries[key] = c.order.PushFront(&tokenEntry{key: key, rows: rows, size: size})
	c.size += size
	for c.size > tokenCacheBytes && c.order.Len() > 1 {
		oldest := c.order.Back()
		entry := oldest.Value.(*tokenEntry)
		c.order.Remove(oldest)
		delete(c.entries, entry.key)
		c.size -= entry.size
	}
}

// tokeniseLines is the pieces of each of the lines, as a language reads them
// together; a language chroma does not know has each line as one piece.
func tokeniseLines(language string, lines []string) [][]codeToken {
	text := strings.Join(lines, "\n")
	key := tokenKey{language: language, lines: len(lines), sum: sha1.Sum([]byte(text))}
	if rows, found := tokensKept.get(key); found {
		return rows
	}
	recognised.Add(1)
	rows := lexRows(language, text, len(lines))
	tokensKept.put(key, rows, len(text))
	return rows
}

func lexRows(language, text string, count int) [][]codeToken {
	rows := make([][]codeToken, 0, count)
	var current []codeToken
	var lexer chroma.Lexer
	if language != "" {
		lexer = lexers.Get(language)
	}
	if lexer != nil {
		if iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text); err == nil {
			for _, token := range iterator.Tokens() {
				parts := strings.Split(token.Value, "\n")
				for i, part := range parts {
					if part != "" {
						current = append(current, codeToken{text: part, kind: token.Type})
					}
					if i < len(parts)-1 {
						rows, current = append(rows, current), nil
					}
				}
			}
			rows = append(rows, current)
			return fitRows(rows, count)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		var row []codeToken
		if line != "" {
			row = []codeToken{{text: line, kind: chroma.Text}}
		}
		rows = append(rows, row)
	}
	return fitRows(rows, count)
}

// fitRows makes the rows as many as the lines: a lexer may end the text with a
// new line of its own.
func fitRows(rows [][]codeToken, count int) [][]codeToken {
	if len(rows) > count {
		return rows[:count]
	}
	for len(rows) < count {
		rows = append(rows, nil)
	}
	return rows
}

// openState follows what a text has open as its lines go by: a triple quote or
// a fence, a raw string or template, a block comment. A guess is enough: it
// only decides where a chunk may end, and a wrong one costs a chunk of a
// different size, never a wrong colour.
type openState struct {
	triple          string
	backtick, block bool
}

func (s *openState) clean() bool { return s.triple == "" && !s.backtick && !s.block }

func (s *openState) feed(line string) {
	for i := 0; i < len(line); {
		rest := line[i:]
		switch {
		case s.block:
			end := strings.Index(rest, "*/")
			if end < 0 {
				return
			}
			s.block, i = false, i+end+2
		case s.triple != "":
			end := strings.Index(rest, s.triple)
			if end < 0 {
				return
			}
			s.triple, i = "", i+end+3
		case s.backtick:
			end := strings.IndexByte(rest, '`')
			if end < 0 {
				return
			}
			s.backtick, i = false, i+end+1
		case strings.HasPrefix(rest, "/*"):
			s.block, i = true, i+2
		case strings.HasPrefix(rest, `"""`):
			s.triple, i = `"""`, i+3
		case strings.HasPrefix(rest, "'''"):
			s.triple, i = "'''", i+3
		case strings.HasPrefix(rest, "```"):
			s.triple, i = "```", i+3
		case rest[0] == '`':
			s.backtick, i = true, i+1
		case (strings.HasPrefix(rest, "//") || rest[0] == '#') && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return
		default:
			i++
		}
	}
}

// safeStart tells whether a chunk may begin at a line: one with no indent
// after a blank one, where a new function or paragraph begins.
func safeStart(lines []string, i int) bool {
	line := lines[i]
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return false
	}
	return i == 0 || strings.TrimSpace(lines[i-1]) == ""
}

// chunkStarts are the lines the chunks of a text begin on: the first, then
// one at the first safe place once a chunk has target lines, or where it
// reaches max whatever the place.
func chunkStarts(lines []string, target, most int) []int {
	starts := []int{0}
	var state openState
	from := 0
	for i := range lines {
		if i > from && ((i-from >= target && state.clean() && safeStart(lines, i)) || i-from >= most) {
			starts, from = append(starts, i), i
		}
		state.feed(lines[i])
	}
	return starts
}

// wrapSpans cuts a line into the pieces that fit inner columns, as runes from
// and to, at least one, so an empty line is a row too.
func wrapSpans(line string, inner int) [][2]int {
	inner = max(inner, 1)
	ascii := true
	for i := 0; i < len(line); i++ {
		if line[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		if len(line) <= inner {
			return [][2]int{{0, len(line)}}
		}
		var spans [][2]int
		for from := 0; from < len(line); from += inner {
			spans = append(spans, [2]int{from, min(from+inner, len(line))})
		}
		return spans
	}
	var spans [][2]int
	from, index, used := 0, 0, 0
	for _, character := range line {
		width := ansi.StringWidth(string(character))
		if used+width > inner && index > from {
			spans, from, used = append(spans, [2]int{from, index}), index, 0
		}
		used += width
		index++
	}
	return append(spans, [2]int{from, index})
}

// sliceRunes is the runes from lo to hi of text.
func sliceRunes(text string, lo, hi int) string {
	if lo <= 0 && hi >= len(text) {
		return text
	}
	start, index := 0, 0
	for offset := range text {
		if index == lo {
			start = offset
		}
		if index == hi {
			return text[start:offset]
		}
		index++
	}
	if lo >= index {
		return ""
	}
	return text[start:]
}

// renderSpan paints the runes from to of a line's pieces on a background.
func renderSpan(row []codeToken, from, to int, background color.Color, colors syntaxColors, styles map[chroma.TokenType]lipgloss.Style) string {
	var out strings.Builder
	position := 0
	for _, token := range row {
		length := utf8.RuneCountInString(token.text)
		if position+length <= from {
			position += length
			continue
		}
		if position >= to {
			break
		}
		piece := sliceRunes(token.text, max(from-position, 0), min(to-position, length))
		style, found := styles[token.kind]
		if !found {
			style = lipgloss.NewStyle().Background(background).Foreground(colors.of(token.kind))
			styles[token.kind] = style
		}
		out.WriteString(style.Render(piece))
		position += length
	}
	return out.String()
}

// codeSpan is a row of the screen: the runes from to of a line.
type codeSpan struct{ line, from, to int }

// codeText is a text of a language cut into the rows of a width, to paint the
// rows asked for.
type codeText struct {
	language string
	lines    []string
	rows     []codeSpan
	chunks   []int
}

// cleanCode is code as it is drawn: tabs four spaces, no styles, no carriage
// returns, and no new lines at its end.
func cleanCode(text string) string {
	text = strings.ReplaceAll(ansi.Strip(text), "\r", "")
	return strings.TrimRight(strings.ReplaceAll(text, "\t", "    "), "\n")
}

func newCodeText(text, language string, inner int) *codeText {
	return newCodeLines(strings.Split(cleanCode(text), "\n"), language, inner)
}

// newCodeLines is a code text of lines as they are, with their tabs and blank
// ones at the end: those of a file read, which numbers them.
func newCodeLines(lines []string, language string, inner int) *codeText {
	c := &codeText{language: language, lines: lines}
	for i, line := range lines {
		for _, span := range wrapSpans(line, inner) {
			c.rows = append(c.rows, codeSpan{line: i, from: span[0], to: span[1]})
		}
	}
	c.chunks = chunkStarts(lines, chunkTarget, chunkMax)
	return c
}

// tokensOf is the pieces of a line, recognised with its chunk.
func (c *codeText) tokensOf(line int) []codeToken {
	chunk := sort.Search(len(c.chunks), func(i int) bool { return c.chunks[i] > line }) - 1
	start, end := c.chunks[chunk], len(c.lines)
	if chunk+1 < len(c.chunks) {
		end = c.chunks[chunk+1]
	}
	return tokeniseLines(c.language, c.lines[start:end])[line-start]
}

// plain is the text of a row.
func (c *codeText) plain(row int) string {
	span := c.rows[row]
	return sliceRunes(c.lines[span.line], span.from, span.to)
}

// render paints rows from to before to, each on the background its row has.
func (c *codeText) render(from, to int, background func(row int) color.Color, dark bool) []string {
	colors := syntaxPalette(dark)
	var out []string
	styles := map[chroma.TokenType]lipgloss.Style{}
	var shade color.Color
	for row := max(from, 0); row < min(to, len(c.rows)); row++ {
		if next := background(row); next != shade {
			shade, styles = next, map[chroma.TokenType]lipgloss.Style{}
		}
		span := c.rows[row]
		out = append(out, renderSpan(c.tokensOf(span.line), span.from, span.to, shade, colors, styles))
	}
	return out
}

// paint is the runes from to of one line, painted on a background.
func (c *codeText) paint(line, from, to int, background color.Color, dark bool) string {
	return renderSpan(c.tokensOf(line), from, to, background, syntaxPalette(dark), map[chroma.TokenType]lipgloss.Style{})
}

// lastLines is the last n lines of a text, found from its end, so a text of
// any length costs what n does.
func lastLines(text string, n int) []string {
	if text == "" {
		return nil
	}
	start, end := 0, len(text)
	for range n {
		i := strings.LastIndexByte(text[:end], '\n')
		if i < 0 {
			start = 0
			break
		}
		start, end = i+1, i
	}
	return strings.Split(text[start:], "\n")
}

// codeTail paints the last want rows of a text that is being written, of
// width inner, on a background: the complete lines recognised together with
// the lines before them, from a safe place a few dozen back, so what began
// above the window is known; the line still being written by itself, as it
// has nothing to be known with. Each row is the code only, to decorate.
func codeTail(text, language string, want, inner int, background color.Color, dark bool) []string {
	complete, partial := "", text
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		complete, partial = text[:i], text[i+1:]
	}
	partial = strings.ReplaceAll(ansi.Strip(partial), "\t", "    ")
	var lines []string
	for _, line := range lastLines(complete, want+tailContext) {
		lines = append(lines, strings.ReplaceAll(ansi.Strip(line), "\t", "    "))
	}
	rows := make([]codeSpan, 0, want)
	// Rows from the end: the partial line is the last, not in lines.
	var partialSpans [][2]int
	if partial != "" {
		partialSpans = wrapSpans(partial, inner)
	}
	needed := want - len(partialSpans)
	if needed < 0 {
		partialSpans = partialSpans[len(partialSpans)-want:]
		needed = 0
	}
	first := len(lines)
	for i := len(lines) - 1; i >= 0 && needed > 0; i-- {
		spans := wrapSpans(lines[i], inner)
		if len(spans) > needed {
			spans = spans[len(spans)-needed:]
		}
		for j := len(spans) - 1; j >= 0; j-- {
			rows = append(rows, codeSpan{line: i, from: spans[j][0], to: spans[j][1]})
		}
		needed -= len(spans)
		first = i
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}

	// Where the recognising begins: a safe place between tailMinContext and
	// tailContext lines before the window, else the farthest.
	start := max(first-tailContext, 0)
	for i := first - tailMinContext; i >= max(first-tailContext, 0); i-- {
		if i >= 0 && safeStart(lines, i) {
			start = i
			break
		}
	}
	region := lines[start:]
	var tokens [][]codeToken
	chunks := chunkStarts(region, tailChunkTarget, tailChunkMax)
	for k, begin := range chunks {
		end := len(region)
		if k+1 < len(chunks) {
			end = chunks[k+1]
		}
		tokens = append(tokens, tokeniseLines(language, region[begin:end])...)
	}

	colors := syntaxPalette(dark)
	styles := map[chroma.TokenType]lipgloss.Style{}
	var out []string
	for _, row := range rows {
		out = append(out, renderSpan(tokens[row.line-start], row.from, row.to, background, colors, styles))
	}
	if len(partialSpans) > 0 {
		alone := tokeniseLines(language, []string{partial})[0]
		for _, span := range partialSpans {
			out = append(out, renderSpan(alone, span[0], span[1], background, colors, styles))
		}
	}
	return out
}

// lazyRowsOver is how many rows a block of code has before the screen paints
// only those in view: below it all are painted at once.
const lazyRowsOver = 400

// lazyRows are the rows of code of a block that the screen paints when it
// needs them: the rows are in the block's text as plain text, which is as
// many lines as painted ones are, and from rows lines from first the screen
// takes those render gives.
type lazyRows struct {
	first, count int
	render       func(from, to int) []string
}

// sgr is a style as the codes that open it and close it, so many short pieces
// are put in it by joining strings, not by rendering each.
type sgr struct{ on, off string }

func newSGR(style lipgloss.Style) sgr {
	on, off, _ := strings.Cut(style.Render("X"), "X")
	return sgr{on: on, off: off}
}

func (s sgr) wrap(text string) string {
	if text == "" {
		return ""
	}
	return s.on + text + s.off
}

// codeBlock is the rows of a code text, each as decorate makes it from the
// code of its row, as a text: painted when they are few, else plain, with the
// function that paints the rows in view as they are asked for.
func codeBlock(code *codeText, background color.Color, dark bool, decorate func(row int, piece string) string) (string, func(from, to int) []string) {
	shade := func(int) color.Color { return background }
	paint := func(from, to int) []string {
		pieces := code.render(from, to, shade, dark)
		rows := make([]string, len(pieces))
		for i, piece := range pieces {
			rows[i] = decorate(max(from, 0)+i, piece)
		}
		return rows
	}
	if len(code.rows) <= lazyRowsOver {
		return strings.Join(paint(0, len(code.rows)), "\n"), nil
	}
	plain := newSGR(lipgloss.NewStyle().Background(background).Foreground(syntaxPalette(dark).text))
	rows := make([]string, len(code.rows))
	for i := range rows {
		rows[i] = decorate(i, plain.wrap(code.plain(i)))
	}
	return strings.Join(rows, "\n"), paint
}
