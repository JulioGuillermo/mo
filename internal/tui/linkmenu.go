package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	linkOpen     = "link:open"
	linkCopy     = "link:copy"
	linkCopyText = "link:text"
)

// link is a web address shown in the conversation, and the text it is
// shown as: the address itself, or a markdown link's words.
type link struct {
	url  string
	text string
}

// linkUnder is the link at x, y of the conversation, if there is one.
func (m model) linkUnder(x, y int) (link, bool) {
	row := y + m.conversationOffset()
	lines := m.conversationLines().window(row, row+1)
	if row < 0 || len(lines) == 0 {
		return link{}, false
	}
	return linkAt(lines[0], x)
}

// linkAt is the link at column x of a drawn line: the hyperlink the
// terminal is told of there, as markdown links and addresses are drawn,
// else an address written as plain text, as in a command's output.
func linkAt(line string, x int) (link, bool) {
	if found, ok := hyperlinkAt(line, x); ok {
		return found, true
	}
	word := strings.TrimLeft(wordAt(ansi.Strip(line), x), "`'\"([{<")
	word = strings.TrimRight(word, "`'\")]}>,;.:!?")
	if strings.HasPrefix(word, "https://") || strings.HasPrefix(word, "http://") {
		return link{url: word, text: word}, true
	}
	return link{}, false
}

// hyperlinkAt reads the OSC 8 hyperlinks of a line, as glamour draws
// links, and gives the one column x falls in, with the text shown for it
// there: the run of that link around x, as glamour draws a link's words
// and its address apart.
func hyperlinkAt(line string, x int) (link, bool) {
	column := 0
	current := ""
	var run strings.Builder
	found := false
	for i := 0; i < len(line); {
		if line[i] == '\x1b' {
			sequence, url, isLink := escapeAt(line[i:])
			if isLink && url != current {
				if found {
					break
				}
				current = url
				run.Reset()
			}
			i += len(sequence)
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		width := ansi.StringWidth(string(r))
		if current != "" {
			run.WriteRune(r)
			if x >= column && x < column+max(width, 1) {
				found = true
			}
		}
		column += width
		i += size
	}
	if !found {
		return link{}, false
	}
	return link{url: current, text: strings.TrimSpace(run.String())}, true
}

// escapeAt is the escape sequence text starts with; for an OSC 8
// hyperlink, the address it opens, empty when it closes one.
func escapeAt(text string) (sequence, url string, isLink bool) {
	if len(text) < 2 {
		return text, "", false
	}
	switch text[1] {
	case '[':
		end := 2
		for end < len(text) && (text[end] < 0x40 || text[end] > 0x7e) {
			end++
		}
		return text[:min(end+1, len(text))], "", false
	case ']':
		end, terminator := len(text), 0
		if bell := strings.IndexByte(text, '\a'); bell >= 0 {
			end, terminator = bell, 1
		}
		if st := strings.Index(text, "\x1b\\"); st >= 0 && st < end {
			end, terminator = st, 2
		}
		body := text[2:end]
		sequence = text[:min(end+terminator, len(text))]
		if rest, ok := strings.CutPrefix(body, "8;"); ok {
			_, url, _ = strings.Cut(rest, ";")
			return sequence, url, true
		}
		return sequence, "", false
	}
	return text[:2], "", false
}

// linkOptions are what a link offers: opening it, copying it, and copying
// the words it is shown as when they are not the address.
func linkOptions(found link) []huh.Option[string] {
	options := []huh.Option[string]{
		huh.NewOption("open it in the browser", linkOpen),
		huh.NewOption("copy the link", linkCopy),
	}
	if found.text != "" && found.text != found.url {
		options = append(options, huh.NewOption("copy its text: "+oneLine(found.text, 40), linkCopyText))
	}
	return options
}

// linkMenu is what a right click on a link offers.
func (m model) linkMenu(found link) (tea.Model, tea.Cmd) {
	return m.showChoices("Link", found.url, linkOptions(found), func(m model, choice string) (tea.Model, tea.Cmd) {
		switch choice {
		case linkOpen:
			return m, openOutside(found.url)
		case linkCopy:
			return m.flashCopied(found.url, "copied the link")
		case linkCopyText:
			return m.flashCopied(found.text, "copied its text")
		}
		return m, nil
	})
}
