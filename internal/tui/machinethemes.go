package tui

// The machine themes are displays of a machine after the end of the world:
// grounds of a night close to black, and on them one colour of every
// strength, with a second and a contrast where one alone would not tell
// things apart. Redline is the first; each theme has a file of its own.

// machinePalette is how a machine theme colours code: primary for what
// drives it, call for what it calls, strings for what it holds, numbers for
// its numbers and builtins, types for its types and paths, and operators.
type machinePalette struct {
	text, comment, primary, call, strings, numbers, types, operators string
}

func machineColors(p machinePalette) syntaxColors {
	return syntaxColors{
		text: plain(p.text), keyword: bold(p.primary), builtin: plain(p.numbers), str: plain(p.strings), comment: italic(p.comment),
		operator: plain(p.operators), punctuation: plain(mixHex(p.text, p.comment, 0.55)), variable: plain(p.text), number: plain(p.numbers),
		function: bold(p.call), typ: plain(p.types), constant: plain(p.numbers), attribute: plain(p.strings),
		program: bold(p.primary), shortFlag: plain(p.strings), longFlag: plain(p.operators), path: tone{color: p.types, underline: true}, mark: bold(p.numbers),
	}
}

// onPaper is a machine theme's light side: the look derived from its paper
// and inks, with its ink where the dark side has its colour: code, what the
// user sent, the status line, tables, headings and line numbers.
func onPaper(paper string, inks basePalette, ink string) uiColors {
	c := derived(paper, inks, ink, false)
	toward := func(share float64) string { return mixHex(paper, ink, share) }
	lighter := func(share float64) string { return mixHex(paper, "#ffffff", share) }
	c.inlineCode, c.inlineCodeBack, c.prompt = ink, toward(0.1), toward(0.14)
	c.segment, c.segmentDim, c.segmentDeep, c.statusBase = toward(0.2), toward(0.12), toward(0.06), lighter(0.4)
	c.separator, c.gauge, c.heading = ink, ink, ink
	c.tableHeader, c.tableEven, c.tableOdd = toward(0.33), toward(0.08), lighter(0.35)
	c.lineNumber, c.lineNumberCursor = toward(0.38), ink
	c.promptSpeed, c.generationSpeed = mixHex(ink, "#ffffff", 0.25), ink
	return c
}
