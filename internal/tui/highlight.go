package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
)

// syntaxColors are the colours of code by what each piece is.
type syntaxColors struct {
	text, keyword, builtin, str, comment, operator, variable, number color.Color
}

func syntaxPalette(dark bool) syntaxColors {
	if dark {
		return syntaxColors{
			text: lipgloss.Color("#c0caf5"), keyword: lipgloss.Color("#bb9af7"), builtin: lipgloss.Color("#7dcfff"),
			str: lipgloss.Color("#9ece6a"), comment: lipgloss.Color("#565f89"), operator: lipgloss.Color("#89ddff"),
			variable: lipgloss.Color("#e0af68"), number: lipgloss.Color("#ff9e64"),
		}
	}
	return syntaxColors{
		text: lipgloss.Color("#3760bf"), keyword: lipgloss.Color("#9854f1"), builtin: lipgloss.Color("#007197"),
		str: lipgloss.Color("#587539"), comment: lipgloss.Color("#848cb5"), operator: lipgloss.Color("#006a83"),
		variable: lipgloss.Color("#8c6c3e"), number: lipgloss.Color("#b15c00"),
	}
}

func (c syntaxColors) of(kind chroma.TokenType) color.Color {
	switch {
	case kind.InCategory(chroma.Comment):
		return c.comment
	case kind.InCategory(chroma.Keyword):
		return c.keyword
	case kind == chroma.NameBuiltin:
		return c.builtin
	case kind == chroma.NameVariable || kind == chroma.LiteralStringInterpol:
		return c.variable
	case kind.InSubCategory(chroma.LiteralString):
		return c.str
	case kind.InSubCategory(chroma.LiteralNumber):
		return c.number
	case kind.InCategory(chroma.Operator) || kind.InCategory(chroma.Punctuation):
		return c.operator
	}
	return c.text
}

// highlightLines colours code of a language and wraps it to width, every
// piece on background so the block stays whole; a language chroma does not
// know is plain text.
func highlightLines(code, language string, width int, background color.Color, dark bool) []string {
	colors := syntaxPalette(dark)
	code = strings.ReplaceAll(strings.TrimRight(ansi.Strip(code), "\n"), "\t", "    ")
	tokens := []chroma.Token{{Type: chroma.Text, Value: code}}
	if lexer := lexers.Get(language); lexer != nil && language != "" {
		if iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code); err == nil {
			tokens = iterator.Tokens()
		}
	}
	width = max(width, 10)
	var lines []string
	var line strings.Builder
	used := 0
	for _, token := range tokens {
		style := lipgloss.NewStyle().Background(background).Foreground(colors.of(token.Type))
		var piece strings.Builder
		flush := func() {
			if piece.Len() > 0 {
				line.WriteString(style.Render(piece.String()))
				piece.Reset()
			}
		}
		for _, character := range token.Value {
			if character == '\n' || used == width {
				flush()
				lines = append(lines, line.String())
				line.Reset()
				used = 0
				if character == '\n' {
					continue
				}
			}
			piece.WriteRune(character)
			used += max(ansi.StringWidth(string(character)), 1)
		}
		flush()
	}
	return append(lines, line.String())
}
