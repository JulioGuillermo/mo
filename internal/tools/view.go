package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	viewDefaultLimit = 2000
	viewDefaultWidth = 2000
	viewProperties   = `"offset":{"type":"integer"},"limit":{"type":"integer"},"col":{"type":"integer"},"width":{"type":"integer"},"match":{"type":"string"},"numbered":{"type":"boolean"}`
)

// View is which part of a tool's output the model asked to see, the same
// for every tool that returns text: limit lines after skipping offset (from
// the end when negative), width bytes of each from byte col, only the lines
// matching match, numbered or not.
type View struct {
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
	Col      int    `json:"col"`
	Width    int    `json:"width"`
	Match    string `json:"match"`
	Numbered bool   `json:"numbered"`
	// given tells whether any of it was in the call.
	given bool
	re    *regexp.Regexp
}

type viewKey struct{}

// ViewFrom is the view of the call a tool runs for, for a tool that applies
// it itself, and whether the call asked for one; the default view without.
func ViewFrom(ctx context.Context) (View, bool) {
	view, ok := ctx.Value(viewKey{}).(View)
	if !ok {
		view, _ = parseView(json.RawMessage(`{}`), viewDefaultLimit)
	}
	return view, view.given
}

// parseView reads the view fields of a call; limit defaults to the tool's.
func parseView(raw json.RawMessage, defaultLimit int) (View, error) {
	var view View
	if err := json.Unmarshal(raw, &view); err != nil {
		return view, fmt.Errorf("invalid arguments: %w", err)
	}
	var present map[string]json.RawMessage
	_ = json.Unmarshal(raw, &present)
	for _, name := range []string{"offset", "limit", "col", "width", "match", "numbered"} {
		if _, ok := present[name]; ok {
			view.given = true
		}
	}
	if view.Limit <= 0 {
		view.Limit = defaultLimit
	}
	if view.Width <= 0 {
		view.Width = viewDefaultWidth
	}
	view.Col = max(view.Col, 0)
	if view.Match != "" {
		re, err := regexp.Compile(view.Match)
		if err != nil {
			return view, fmt.Errorf("match: %w", err)
		}
		view.re = re
	}
	return view, nil
}

// Apply cuts text to the view and says what was left out and how to see it.
func (v View) Apply(text string) string {
	if text == "" {
		return text
	}
	picker := v.picker()
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		picker.add(i, line, len(line))
	}
	return picker.result()
}

// viewPicker takes a text's lines one at a time and keeps only those its
// view shows, so a text too big to hold is seen the same as a small one.
type viewPicker struct {
	view View
	// seen counts the lines the view picks from: all, or those matching.
	seen int
	// kept are the lines shown, cut to the view's columns: from offset on,
	// or with a negative offset the last ones seen, as many as it asks.
	kept []keptLine
	// oldest is where the ring of a negative offset starts in kept.
	oldest int
}

type keptLine struct {
	number int
	text   string
}

func (v View) picker() *viewPicker {
	return &viewPicker{view: v}
}

// add takes the line at number, of full bytes, of which line may hold only
// the start.
func (p *viewPicker) add(number int, line string, full int) {
	v := p.view
	if v.re != nil && !v.re.MatchString(line) {
		return
	}
	index := p.seen
	p.seen++
	if v.Offset >= 0 && (index < v.Offset || index >= v.Offset+v.Limit) {
		return
	}
	kept := keptLine{number, cutLine(line, full, v.Col, v.Width)}
	if v.Offset < 0 && len(p.kept) == -v.Offset {
		p.kept[p.oldest] = kept
		p.oldest = (p.oldest + 1) % len(p.kept)
		return
	}
	p.kept = append(p.kept, kept)
}

// each is add for eachLine, which goes on to the end.
func (p *viewPicker) each(number int, line string, full int) bool {
	p.add(number, line, full)
	return true
}

// result is what the view shows of the lines added, as Apply says it.
func (p *viewPicker) result() string {
	v := p.view
	unit := "lines"
	if v.re != nil {
		unit = "matching lines"
		if p.seen == 0 {
			return fmt.Sprintf("no line matches %q", v.Match)
		}
	}
	start := v.Offset
	if start < 0 {
		start = max(p.seen+start, 0)
	}
	if start >= p.seen {
		return fmt.Sprintf("offset %d is past the end: %d %s", v.Offset, p.seen, unit)
	}
	end := min(start+v.Limit, p.seen)
	shown := p.kept
	if v.Offset < 0 {
		shown = append(slices.Clone(p.kept[p.oldest:]), p.kept[:p.oldest]...)
		first := start - (p.seen - len(p.kept))
		shown = shown[first : first+end-start]
	}
	var out strings.Builder
	for _, line := range shown {
		if v.Numbered || v.re != nil {
			fmt.Fprintf(&out, "%d\t", line.number+1)
		}
		out.WriteString(line.text)
		out.WriteByte('\n')
	}
	if start > 0 || end < p.seen {
		next := ""
		if end < p.seen {
			next = fmt.Sprintf("; offset=%d continues", end)
		}
		fmt.Fprintf(&out, "[%s %d-%d of %d%s]\n", unit, start+1, end, p.seen, next)
	}
	return out.String()
}

// cutView is width bytes of a line from byte col, saying how many bytes are
// left after them and the col to read on from.
func cutView(line string, col, width int) string {
	return cutLine(line, len(line), col, width)
}

// cutLine is cutView of a line of full bytes of which line holds the start,
// at least col and width of it.
func cutLine(line string, full, col, width int) string {
	if col > 0 {
		if col >= len(line) {
			return ""
		}
		for col < len(line) && !utf8.RuneStart(line[col]) {
			col++
		}
		line = line[col:]
	}
	if full-col <= width {
		return line
	}
	part := cutAtRune(line, width)
	return part + fmt.Sprintf(" [%d bytes cut; col=%d continues]", full-col-len(part), col+len(part))
}

// withViewSchema adds the view fields after a tool's own parameters.
func withViewSchema(schema json.RawMessage) json.RawMessage {
	text := string(schema)
	open := strings.Index(text, `"properties":{`)
	if open < 0 {
		return schema
	}
	open += len(`"properties":`)
	depth, quoted, escaped := 0, false, false
	for i := open; i < len(text); i++ {
		c := text[i]
		switch {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				separator := ","
				if strings.TrimSpace(text[open+1:i]) == "" {
					separator = ""
				}
				return json.RawMessage(text[:i] + separator + viewProperties + text[i:])
			}
		}
	}
	return schema
}
