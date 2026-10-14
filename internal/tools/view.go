package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	numbers := make([]int, 0, len(lines))
	for i, line := range lines {
		if v.re == nil || v.re.MatchString(line) {
			numbers = append(numbers, i)
		}
	}
	unit := "lines"
	if v.re != nil {
		unit = "matching lines"
		if len(numbers) == 0 {
			return fmt.Sprintf("no line matches %q", v.Match)
		}
	}
	start := v.Offset
	if start < 0 {
		start = max(len(numbers)+start, 0)
	}
	if start >= len(numbers) {
		return fmt.Sprintf("offset %d is past the end: %d %s", v.Offset, len(numbers), unit)
	}
	end := min(start+v.Limit, len(numbers))
	var out strings.Builder
	for _, i := range numbers[start:end] {
		if v.Numbered || v.re != nil {
			fmt.Fprintf(&out, "%d\t", i+1)
		}
		out.WriteString(cutView(lines[i], v.Col, v.Width))
		out.WriteByte('\n')
	}
	if start > 0 || end < len(numbers) {
		next := ""
		if end < len(numbers) {
			next = fmt.Sprintf("; offset=%d continues", end)
		}
		fmt.Fprintf(&out, "[%s %d-%d of %d%s]\n", unit, start+1, end, len(numbers), next)
	}
	return out.String()
}

// cutView is width bytes of a line from byte col, saying how many bytes are
// left after them and the col to read on from.
func cutView(line string, col, width int) string {
	if col > 0 {
		if col >= len(line) {
			return ""
		}
		for col < len(line) && !utf8.RuneStart(line[col]) {
			col++
		}
		line = line[col:]
	}
	if len(line) <= width {
		return line
	}
	part := cutAtRune(line, width)
	return part + fmt.Sprintf(" [%d bytes cut; col=%d continues]", len(line)-len(part), col+len(part))
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
