package tui

import (
	"sync/atomic"

	"charm.land/glamour/v2/ansi"
)

// syntaxTheme is a way of colouring code, with a look for dark screens and
// one for light ones; styled sets it on the code blocks of answers too,
// which otherwise keep glamour's own.
type syntaxTheme struct {
	name, about string
	dark, light syntaxColors
	styled      bool
}

func (t *syntaxTheme) colors(dark bool) syntaxColors {
	if dark {
		return t.dark
	}
	return t.light
}

// basePalette is a theme's colours, by hue, each #rrggbb.
type basePalette struct {
	text, comment, red, orange, yellow, green, teal, cyan, blue, purple, pink string
}

func plain(color string) tone  { return tone{color: color} }
func bold(color string) tone   { return tone{color: color, bold: true} }
func italic(color string) tone { return tone{color: color, italic: true} }

// shellTones are the tones of a shell command the colourful themes share:
// the program bold, short flags yellow, long ones purple, paths underlined.
func shellTones(c syntaxColors, p basePalette) syntaxColors {
	c.program = bold(p.blue)
	c.shortFlag = plain(p.yellow)
	c.longFlag = plain(p.purple)
	c.path = tone{color: p.cyan, underline: true}
	c.mark = bold(p.orange)
	return c
}

// oneLike colours code as One Dark does, and most themes after it.
func oneLike(p basePalette) syntaxColors {
	return shellTones(syntaxColors{
		text: plain(p.text), keyword: plain(p.purple), builtin: plain(p.cyan), str: plain(p.green), comment: italic(p.comment),
		operator: plain(p.cyan), punctuation: plain(p.text), variable: plain(p.red), number: plain(p.orange),
		function: plain(p.blue), typ: plain(p.yellow), constant: plain(p.orange), attribute: plain(p.orange),
	}, p)
}

var (
	tokyoNight = basePalette{text: "#c0caf5", comment: "#565f89", red: "#f7768e", orange: "#ff9e64", yellow: "#e0af68",
		green: "#9ece6a", teal: "#73daca", cyan: "#7dcfff", blue: "#7aa2f7", purple: "#bb9af7", pink: "#ff007c"}
	tokyoDay = basePalette{text: "#3760bf", comment: "#848cb5", red: "#f52a65", orange: "#b15c00", yellow: "#8c6c3e",
		green: "#587539", teal: "#118c74", cyan: "#007197", blue: "#2e7de9", purple: "#9854f1", pink: "#d20065"}
	oneDark = basePalette{text: "#abb2bf", comment: "#5c6370", red: "#e06c75", orange: "#d19a66", yellow: "#e5c07b",
		green: "#98c379", teal: "#56b6c2", cyan: "#56b6c2", blue: "#61afef", purple: "#c678dd", pink: "#c678dd"}
	oneLight = basePalette{text: "#383a42", comment: "#a0a1a7", red: "#e45649", orange: "#986801", yellow: "#c18401",
		green: "#50a14f", teal: "#0184bc", cyan: "#0184bc", blue: "#4078f2", purple: "#a626a4", pink: "#a626a4"}
	mocha = basePalette{text: "#cdd6f4", comment: "#7f849c", red: "#f38ba8", orange: "#fab387", yellow: "#f9e2af",
		green: "#a6e3a1", teal: "#94e2d5", cyan: "#89dceb", blue: "#89b4fa", purple: "#cba6f7", pink: "#f5c2e7"}
	latte = basePalette{text: "#4c4f69", comment: "#8c8fa1", red: "#d20f39", orange: "#fe640b", yellow: "#df8e1d",
		green: "#40a02b", teal: "#179299", cyan: "#04a5e5", blue: "#1e66f5", purple: "#8839ef", pink: "#ea76cb"}
	materialDark = basePalette{text: "#eeffff", comment: "#546e7a", red: "#f07178", orange: "#f78c6c", yellow: "#ffcb6b",
		green: "#c3e88d", teal: "#89ddff", cyan: "#89ddff", blue: "#82aaff", purple: "#c792ea", pink: "#ff5370"}
	materialLight = basePalette{text: "#546e7a", comment: "#90a4ae", red: "#e53935", orange: "#f76d47", yellow: "#f6a434",
		green: "#91b859", teal: "#39adb5", cyan: "#39adb5", blue: "#6182b8", purple: "#7c4dff", pink: "#ff5370"}
	dracula = basePalette{text: "#f8f8f2", comment: "#6272a4", red: "#ff5555", orange: "#ffb86c", yellow: "#f1fa8c",
		green: "#50fa7b", teal: "#8be9fd", cyan: "#8be9fd", blue: "#bd93f9", purple: "#bd93f9", pink: "#ff79c6"}
	alucard = basePalette{text: "#1f1f1f", comment: "#6c664b", red: "#cb3a2a", orange: "#a34d14", yellow: "#846e15",
		green: "#14710a", teal: "#036a96", cyan: "#036a96", blue: "#644ac9", purple: "#644ac9", pink: "#a3144d"}
	gruvboxDark = basePalette{text: "#ebdbb2", comment: "#928374", red: "#fb4934", orange: "#fe8019", yellow: "#fabd2f",
		green: "#b8bb26", teal: "#8ec07c", cyan: "#8ec07c", blue: "#83a598", purple: "#d3869b", pink: "#d3869b"}
	gruvboxLight = basePalette{text: "#3c3836", comment: "#928374", red: "#9d0006", orange: "#af3a03", yellow: "#b57614",
		green: "#79740e", teal: "#427b58", cyan: "#427b58", blue: "#076678", purple: "#8f3f71", pink: "#8f3f71"}
	nordDark = basePalette{text: "#d8dee9", comment: "#616e88", red: "#bf616a", orange: "#d08770", yellow: "#ebcb8b",
		green: "#a3be8c", teal: "#8fbcbb", cyan: "#88c0d0", blue: "#81a1c1", purple: "#b48ead", pink: "#b48ead"}
	nordLight = basePalette{text: "#2e3440", comment: "#7b88a1", red: "#bf616a", orange: "#c5727a", yellow: "#a88438",
		green: "#6b8e4e", teal: "#4c8a87", cyan: "#3b7f99", blue: "#5e81ac", purple: "#9a6d96", pink: "#9a6d96"}
	solarizedDark = basePalette{text: "#93a1a1", comment: "#586e75", red: "#dc322f", orange: "#cb4b16", yellow: "#b58900",
		green: "#859900", teal: "#2aa198", cyan: "#2aa198", blue: "#268bd2", purple: "#6c71c4", pink: "#d33682"}
	solarizedLight = basePalette{text: "#586e75", comment: "#93a1a1", red: "#dc322f", orange: "#cb4b16", yellow: "#b58900",
		green: "#859900", teal: "#2aa198", cyan: "#2aa198", blue: "#268bd2", purple: "#6c71c4", pink: "#d33682"}
	monokaiDark = basePalette{text: "#f8f8f2", comment: "#75715e", red: "#f92672", orange: "#fd971f", yellow: "#e6db74",
		green: "#a6e22e", teal: "#66d9ef", cyan: "#66d9ef", blue: "#66d9ef", purple: "#ae81ff", pink: "#f92672"}
	monokaiLight = basePalette{text: "#29242a", comment: "#a59fa0", red: "#e14775", orange: "#e16032", yellow: "#cc7a0a",
		green: "#269d69", teal: "#1c8ca8", cyan: "#1c8ca8", blue: "#1c8ca8", purple: "#7058be", pink: "#e14775"}
)

// moColors are mo's colours as they have always been: few, as chroma sorts
// the pieces, with the programs of commands cyan.
func moColors(p basePalette, operator, variable string) syntaxColors {
	keyword := plain(p.purple)
	return syntaxColors{
		text: plain(p.text), keyword: keyword, builtin: plain(p.cyan), str: plain(p.green), comment: plain(p.comment),
		operator: plain(operator), punctuation: plain(operator), variable: plain(variable), number: plain(p.orange),
		function: plain(p.text), typ: keyword, constant: keyword, attribute: plain(p.text),
		program: plain(p.cyan), shortFlag: plain(p.text), longFlag: plain(p.text), path: plain(p.text), mark: plain(variable),
	}
}

// vivid is mo's colours with every piece told apart: what the shell input
// first showed.
func vivid(p basePalette) syntaxColors {
	c := oneLike(p)
	c.keyword = italic(p.purple)
	c.function = bold(p.blue)
	c.typ = plain(p.teal)
	c.variable = plain(p.teal)
	c.operator = plain(p.red)
	c.punctuation = plain(p.comment)
	c.attribute = plain(p.yellow)
	return c
}

// tokyoColors are Tokyo Night's own choices.
func tokyoColors(p basePalette) syntaxColors {
	c := oneLike(p)
	c.keyword = italic(p.purple)
	c.typ = plain(p.teal)
	c.variable = plain(p.text)
	c.attribute = plain(p.green)
	c.punctuation = plain(p.cyan)
	return c
}

func catppuccinColors(p basePalette) syntaxColors {
	c := oneLike(p)
	c.builtin = plain(p.red)
	c.variable = plain(p.text)
	c.operator = plain(p.cyan)
	c.punctuation = plain(p.comment)
	c.attribute = plain(p.yellow)
	return c
}

func draculaColors(p basePalette) syntaxColors {
	c := oneLike(p)
	c.keyword = plain(p.pink)
	c.function = plain(p.green)
	c.typ = italic(p.cyan)
	c.constant, c.number = plain(p.purple), plain(p.purple)
	c.str = plain(p.yellow)
	c.variable = plain(p.text)
	c.operator = plain(p.pink)
	c.attribute = plain(p.green)
	c.program = bold(p.green)
	return c
}

func gruvboxColors(p basePalette) syntaxColors {
	c := oneLike(p)
	c.keyword = plain(p.red)
	c.function = bold(p.green)
	c.constant, c.number = plain(p.purple), plain(p.purple)
	c.builtin = plain(p.orange)
	c.variable = plain(p.blue)
	c.operator = plain(p.text)
	c.attribute = plain(p.teal)
	c.program = bold(p.green)
	return c
}

func nordColors(p basePalette) syntaxColors {
	c := oneLike(p)
	c.keyword = plain(p.blue)
	c.function = plain(p.cyan)
	c.typ = plain(p.teal)
	c.constant, c.number = plain(p.purple), plain(p.purple)
	c.builtin = plain(p.blue)
	c.variable = plain(p.text)
	c.operator = plain(p.blue)
	c.attribute = plain(p.teal)
	c.program = bold(p.cyan)
	return c
}

func solarizedColors(p basePalette) syntaxColors {
	c := oneLike(p)
	c.keyword = plain(p.green)
	c.typ = plain(p.yellow)
	c.constant = plain(p.cyan)
	c.number = plain(p.pink)
	c.str = plain(p.cyan)
	c.builtin = plain(p.orange)
	c.variable = plain(p.blue)
	c.operator = plain(p.green)
	return c
}

func monokaiColors(p basePalette) syntaxColors {
	c := draculaColors(p)
	c.typ = italic(p.cyan)
	c.str = plain(p.yellow)
	c.keyword = plain(p.red)
	return c
}

// syntaxThemes are the ways code can be coloured, mo's own first.
var syntaxThemes = []*syntaxTheme{
	{name: "mo", about: "mo's own, as it has always been", dark: moColors(tokyoNight, "#89ddff", "#e0af68"), light: moColors(tokyoDay, "#006a83", "#8c6c3e")},
	{name: "mo vivid", about: "mo's colours with every piece told apart", dark: vivid(tokyoNight), light: vivid(tokyoDay), styled: true},
	{name: "redline", about: "reds, amber and white, steel blue types", dark: machineColors(redlineNight), light: machineColors(redlineAsh), styled: true},
	{name: "coldwire", about: "cyan, ice and cobalt, periwinkle types", dark: machineColors(coldwireNight), light: machineColors(coldwireFrost), styled: true},
	{name: "neon requiem", about: "hot pink, lavender and violet, neon cyan types", dark: machineColors(neonRequiemNight), light: machineColors(neonRequiemDawn), styled: true},
	{name: "fallout", about: "radiation green, rust orange and lime", dark: machineColors(falloutNight), light: machineColors(falloutDust), styled: true},
	{name: "tokyo night", about: "Tokyo Night and Tokyo Night Day", dark: tokyoColors(tokyoNight), light: tokyoColors(tokyoDay), styled: true},
	{name: "one", about: "One Dark and One Light", dark: oneLike(oneDark), light: oneLike(oneLight), styled: true},
	{name: "catppuccin", about: "Catppuccin Mocha and Latte", dark: catppuccinColors(mocha), light: catppuccinColors(latte), styled: true},
	{name: "material", about: "Material and Material Lighter", dark: oneLike(materialDark), light: oneLike(materialLight), styled: true},
	{name: "dracula", about: "Dracula and Alucard", dark: draculaColors(dracula), light: draculaColors(alucard), styled: true},
	{name: "gruvbox", about: "Gruvbox dark and light", dark: gruvboxColors(gruvboxDark), light: gruvboxColors(gruvboxLight), styled: true},
	{name: "nord", about: "Nord, and a light one of its colours", dark: nordColors(nordDark), light: nordColors(nordLight), styled: true},
	{name: "solarized", about: "Solarized dark and light", dark: solarizedColors(solarizedDark), light: solarizedColors(solarizedLight), styled: true},
	{name: "monokai", about: "Monokai and Monokai Pro Light", dark: monokaiColors(monokaiDark), light: monokaiColors(monokaiLight), styled: true},
}

var chosenSyntax atomic.Pointer[syntaxTheme]

func currentSyntax() *syntaxTheme {
	if theme := chosenSyntax.Load(); theme != nil {
		return theme
	}
	return syntaxThemes[0]
}

// syntaxThemeNamed is the theme of that name, mo's own for any other.
func syntaxThemeNamed(name string) *syntaxTheme {
	for _, theme := range syntaxThemes {
		if theme.name == name {
			return theme
		}
	}
	return syntaxThemes[0]
}

// useSyntaxTheme colours code from now on as the theme named does; it
// tells whether that changed anything.
func useSyntaxTheme(name string) bool {
	theme := syntaxThemeNamed(name)
	return chosenSyntax.Swap(theme) != theme
}

// glamourChroma is the theme for the code blocks of answers.
func (c syntaxColors) glamourChroma(base *ansi.Chroma) *ansi.Chroma {
	chroma := ansi.Chroma{}
	if base != nil {
		chroma.Background = base.Background
	}
	set := func(field *ansi.StylePrimitive, t tone) {
		color := t.color
		*field = ansi.StylePrimitive{Color: &color}
		if t.bold {
			field.Bold = &t.bold
		}
		if t.italic {
			field.Italic = &t.italic
		}
	}
	set(&chroma.Text, c.text)
	set(&chroma.Name, c.text)
	set(&chroma.NameOther, c.text)
	set(&chroma.Comment, c.comment)
	set(&chroma.CommentPreproc, c.keyword)
	set(&chroma.Keyword, c.keyword)
	set(&chroma.KeywordReserved, c.keyword)
	set(&chroma.KeywordNamespace, c.keyword)
	set(&chroma.KeywordType, c.typ)
	set(&chroma.Operator, c.operator)
	set(&chroma.Punctuation, c.punctuation)
	set(&chroma.NameBuiltin, c.builtin)
	set(&chroma.NameTag, c.attribute)
	set(&chroma.NameAttribute, c.attribute)
	set(&chroma.NameDecorator, c.attribute)
	set(&chroma.NameClass, c.typ)
	set(&chroma.NameException, c.typ)
	set(&chroma.NameConstant, c.constant)
	set(&chroma.NameFunction, c.function)
	set(&chroma.Literal, c.str)
	set(&chroma.LiteralNumber, c.number)
	set(&chroma.LiteralDate, c.number)
	set(&chroma.LiteralString, c.str)
	set(&chroma.LiteralStringEscape, c.variable)
	set(&chroma.GenericInserted, c.str)
	set(&chroma.GenericDeleted, c.variable)
	return &chroma
}
