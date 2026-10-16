package render

import (
	"regexp"
	"strings"
)

// flowLinks are links of mermaid's flowcharts mermaid-ascii does not read,
// with the ones it does that mean the most like them: words written in a
// link go in its |label|, longer links are drawn as the plain ones, dotted
// ones without a head and both-way dotted or thick ones as their plain
// kind.
var flowLinks = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile(`<--\s+([^-|>][^|]*?)\s+-->`), "<-->|$1|"},
	{regexp.MustCompile(`(^|\s)--\s+([^-|>][^|]*?)\s+-{2,}>`), "$1-->|$2|"},
	{regexp.MustCompile(`(^|\s)--\s+([^-|>][^|]*?)\s+-{3,}(\s|$)`), "$1---|$2|$3"},
	{regexp.MustCompile(`(^|\s)==\s+([^=|>][^|]*?)\s+={2,}>`), "$1==>|$2|"},
	{regexp.MustCompile(`(^|\s)-\.\s+([^.|>][^|]*?)\s+\.-+>`), "$1-.->|$2|"},
	{regexp.MustCompile(`(^|\s)-\.\s+([^.|>][^|]*?)\s+\.-+(\s|$)`), "$1---|$2|$3"},
	{regexp.MustCompile(`<-\.+->|<={2,}>`), "<-->"},
	{regexp.MustCompile(`-\.{2,}->`), "-.->"},
	{regexp.MustCompile(`-\.+-(\s|\||$)`), "---$1"},
	{regexp.MustCompile(`-{3,}>`), "-->"},
	{regexp.MustCompile(`={3,}>`), "==>"},
}

// flowShapes are node shapes of mermaid's flowcharts mermaid-ascii does
// not read, as the box, round box or decision it does: a stadium, a
// subroutine, a cylinder, circles, a hexagon, parallelograms and
// trapezoids, an asymmetric shape.
var flowShapes = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile(`(\w)\(\(\((.*?)\)\)\)`), "$1($2)"},
	{regexp.MustCompile(`(\w)\(\((.*?)\)\)`), "$1($2)"},
	{regexp.MustCompile(`(\w)\(\[(.*?)\]\)`), "$1($2)"},
	{regexp.MustCompile(`(\w)\[\[(.*?)\]\]`), "$1[$2]"},
	{regexp.MustCompile(`(\w)\[\((.*?)\)\]`), "$1[$2]"},
	{regexp.MustCompile(`(\w)\{\{(.*?)\}\}`), "$1{$2}"},
	{regexp.MustCompile(`(\w)\[[/\\](.*?)[/\\]\]`), "$1[$2]"},
	{regexp.MustCompile(`(\w)>([^\]>|]*)\]`), "$1[$2]"},
}

// invisibleLink is ~~~, a link only for layout.
var invisibleLink = regexp.MustCompile(`^(\s*)(\S.*?)\s*~{3,}\s*(\S.*?)\s*$`)

// readableFlowchart writes a flowchart's links as mermaid-ascii reads
// them; other diagrams stay as they are.
func readableFlowchart(source string) string {
	lines := strings.Split(source, "\n")
	flowchart := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "%%") {
			continue
		}
		if !flowchart {
			if !strings.HasPrefix(trimmed, "graph") && !strings.HasPrefix(trimmed, "flowchart") {
				return source
			}
			flowchart = true
			continue
		}
		if parts := invisibleLink.FindStringSubmatch(line); parts != nil {
			lines[i] = parts[1] + parts[2] + "\n" + parts[1] + parts[3]
			continue
		}
		for _, link := range flowLinks {
			line = link.pattern.ReplaceAllString(line, link.with)
		}
		for _, shape := range flowShapes {
			line = shape.pattern.ReplaceAllString(line, shape.with)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
