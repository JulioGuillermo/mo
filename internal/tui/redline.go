package tui

// Redline is a machine's display on alert: grounds of a night blue close to
// black, and on them reds of every strength, with amber and white where red
// alone would not tell things apart, and a cold steel blue against them.

var (
	redlineNight = basePalette{text: "#f2dede", comment: "#56628a", red: "#ff2a3d", orange: "#ff7a3d", yellow: "#ffb347",
		green: "#3fd9c0", teal: "#3fd9c0", cyan: "#6fb7d8", blue: "#6fb7d8", purple: "#ff5a8a", pink: "#ff4fa0"}
	redlineAsh = basePalette{text: "#2a1418", comment: "#7a8199", red: "#c8102e", orange: "#c2410c", yellow: "#a35a00",
		green: "#0f7a6c", teal: "#0f7a6c", cyan: "#1d5f80", blue: "#1d5f80", purple: "#b0124f", pink: "#b0124f"}
)

var (
	redlineDark = uiColors{
		background: "#03060f", sidebar: "#060b19", statusBase: "#0a1024",
		panel: "#151a2e", block: "#0b1122", body: "#0f172e", button: "#3a1020", tab: "#161d38", done: "#0c1326",
		segment: "#1d2238", segmentDim: "#161a2e", segmentDeep: "#10142a",
		peer: "#151a2a", popup: "#080c18", queued: "#2a1a0e",
		cursorLine: "#1c0d18", blockCursor: "#3a0f1c", vimCursor: "#30101a",
		added: "#062420", removed: "#330a12", addedCursor: "#0c3a33", removedCursor: "#55111f",
		inlineCode: "#ffb347", inlineCodeBack: "#1a1424", codeBlockBack: "#080e1e",
		text: "#f2dede", subtext: "#d9a3a8", dim: "#8a5a63", faint: "#5c3d4a", empty: "#2a2236", queuedDim: "#a07a52",
		onAccent: "#03060f",
		accent:   "#ff2a3d", warning: "#ffaa33", green: "#3fd9c0", red: "#ff5470", purple: "#ff5a8a", cyan: "#6fb7d8",
		teal: "#3fd9c0", orange: "#ff7a3d", pink: "#ff4fa0",
	}
	redlineLight = derived("#f6efec", redlineAsh, redlineAsh.red, false)
)

// redlineColors colour code with red for what drives it, white for what it
// calls, amber for what it holds and steel blue for its types.
func redlineColors(p basePalette, call string) syntaxColors {
	return syntaxColors{
		text: plain(p.text), keyword: bold(p.red), builtin: plain(p.orange), str: plain(p.yellow), comment: italic(p.comment),
		operator: plain(p.purple), punctuation: plain(mixHex(p.text, p.comment, 0.55)), variable: plain(p.text), number: plain(p.orange),
		function: bold(call), typ: plain(p.cyan), constant: plain(p.orange), attribute: plain(p.yellow),
		program: bold(p.red), shortFlag: plain(p.yellow), longFlag: plain(p.purple), path: tone{color: p.cyan, underline: true}, mark: bold(p.orange),
	}
}
