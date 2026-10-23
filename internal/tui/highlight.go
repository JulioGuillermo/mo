package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
)

// tone is how a kind of code looks: its colour, as #rrggbb, and its weight.
type tone struct {
	color                   string
	bold, italic, underline bool
}

func (t tone) fg() color.Color {
	return lipgloss.Color(t.color)
}

// on is the tone as a style on a background.
func (t tone) on(background color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(background).Foreground(t.fg()).Bold(t.bold).Italic(t.italic).Underline(t.underline)
}

// syntaxColors are the tones of code by what each piece is; program,
// shortFlag, longFlag and path are of the commands of the shell, and mark
// of the ! before one typed.
type syntaxColors struct {
	text, keyword, builtin, str, comment, operator, punctuation, variable, number tone
	function, typ, constant, attribute                                            tone
	program, shortFlag, longFlag, path, mark                                      tone
}

// Kinds of the pieces of a shell command chroma has no name for.
const (
	kindProgram chroma.TokenType = 900000 + iota
	kindShortFlag
	kindLongFlag
	kindPath
)

func syntaxPalette(dark bool) syntaxColors {
	return currentSyntax().colors(dark)
}

func (c syntaxColors) of(kind chroma.TokenType) tone {
	switch {
	case kind == kindProgram:
		return c.program
	case kind == kindShortFlag:
		return c.shortFlag
	case kind == kindLongFlag:
		return c.longFlag
	case kind == kindPath:
		return c.path
	case kind.InCategory(chroma.Comment):
		return c.comment
	case kind == chroma.KeywordType:
		return c.typ
	case kind == chroma.KeywordConstant:
		return c.constant
	case kind.InCategory(chroma.Keyword):
		return c.keyword
	case kind == chroma.NameBuiltin || kind == chroma.NameBuiltinPseudo:
		return c.builtin
	case kind == chroma.NameFunction || kind == chroma.NameFunctionMagic:
		return c.function
	case kind == chroma.NameClass || kind == chroma.NameNamespace || kind == chroma.NameException:
		return c.typ
	case kind == chroma.NameConstant:
		return c.constant
	case kind == chroma.NameAttribute || kind == chroma.NameTag || kind == chroma.NameDecorator || kind == chroma.NameProperty:
		return c.attribute
	case kind == chroma.NameVariable || kind.InSubCategory(chroma.NameVariable) || kind == chroma.LiteralStringInterpol:
		return c.variable
	case kind.InSubCategory(chroma.LiteralString):
		return c.str
	case kind.InSubCategory(chroma.LiteralNumber):
		return c.number
	case kind.InCategory(chroma.Operator):
		return c.operator
	case kind.InCategory(chroma.Punctuation):
		return c.punctuation
	}
	return c.text
}

// highlightLines colours code of a language and wraps it to width, every
// piece on background so the block stays whole; a language chroma does not
// know is plain text.
func highlightLines(code, language string, width int, background color.Color, dark bool) []string {
	return highlightWith(syntaxPalette(dark), code, language, width, background)
}

func highlightWith(colors syntaxColors, code, language string, width int, background color.Color) []string {
	code = strings.ReplaceAll(strings.TrimRight(ansi.Strip(code), "\n"), "\t", "    ")
	tokens := []chroma.Token{{Type: chroma.Text, Value: code}}
	switch {
	case language == shellLanguage:
		tokens = shellTokens(code)
	case language != "":
		if lexer := lexers.Get(language); lexer != nil {
			if iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code); err == nil {
				tokens = iterator.Tokens()
			}
		}
	}
	width = max(width, 10)
	var lines []string
	var line strings.Builder
	used := 0
	for _, token := range tokens {
		style := colors.of(token.Type).on(background)
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
	lines = append(lines, line.String())
	if len(lines) > 1 && ansi.Strip(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
