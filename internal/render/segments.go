package render

import (
	"regexp"
	"strings"
)

type SegmentKind int

const (
	SegmentText SegmentKind = iota
	SegmentMermaid
	SegmentSVG
	SegmentImageFile
)

type Segment struct {
	Kind    SegmentKind
	Content string
}

var (
	fenceLine = regexp.MustCompile("^\\s*(`{3,}|~{3,})\\s*([^`\\s]*)")
	imageLine = regexp.MustCompile(`^\s*!\[[^\]]*\]\(([^)\s]+)\)\s*$`)
)

// Split cuts markdown into the parts drawn differently: mermaid and svg code
// blocks, lines that are only an image of a local file, and the text around
// them. Inside any other code block nothing is drawn. A block left open runs
// to the end.
func Split(markdown string) []Segment {
	var segments []Segment
	var text, block strings.Builder
	var blockKind SegmentKind
	visualFence, codeFence := "", ""
	flushText := func() {
		if strings.TrimSpace(text.String()) != "" {
			segments = append(segments, Segment{Kind: SegmentText, Content: text.String()})
		}
		text.Reset()
	}
	for line := range strings.Lines(markdown) {
		trimmed := strings.TrimRight(line, "\r\n")
		match := fenceLine.FindStringSubmatch(trimmed)
		switch {
		case visualFence != "":
			if closes(match, visualFence) {
				segments = append(segments, Segment{Kind: blockKind, Content: block.String()})
				block.Reset()
				visualFence = ""
				continue
			}
			block.WriteString(line)
		case codeFence != "":
			if closes(match, codeFence) {
				codeFence = ""
			}
			text.WriteString(line)
		case match != nil && (match[2] == "mermaid" || match[2] == "svg"):
			flushText()
			visualFence = match[1]
			blockKind = SegmentMermaid
			if match[2] == "svg" {
				blockKind = SegmentSVG
			}
		case match != nil:
			codeFence = match[1]
			text.WriteString(line)
		default:
			if image := imageLine.FindStringSubmatch(trimmed); image != nil && isLocalPath(image[1]) {
				flushText()
				segments = append(segments, Segment{Kind: SegmentImageFile, Content: image[1]})
				continue
			}
			text.WriteString(line)
		}
	}
	if visualFence != "" {
		segments = append(segments, Segment{Kind: blockKind, Content: block.String()})
	}
	flushText()
	return segments
}

// closes tells whether a fence line ends a block opened with open: the same
// character, at least as long, and nothing after it.
func closes(match []string, open string) bool {
	return match != nil && match[2] == "" && match[1][0] == open[0] && len(match[1]) >= len(open)
}

func isLocalPath(target string) bool {
	return !strings.Contains(target, "://") && !strings.HasPrefix(target, "data:")
}
