package tui

// Redline is a machine's display on alert: grounds of a night blue close to
// black, and on them reds of every strength, with amber and white where red
// alone would not tell things apart, and a cold steel blue against them.

var (
	redlineNight = machinePalette{text: "#f2dede", comment: "#56628a", primary: "#ff2a3d", call: "#ffffff",
		strings: "#ffb347", numbers: "#ff7a3d", types: "#6fb7d8", operators: "#ff5a8a"}
	redlineAsh = machinePalette{text: "#2a1418", comment: "#7a8199", primary: "#c8102e", call: "#1a0a0e",
		strings: "#a35a00", numbers: "#c2410c", types: "#1d5f80", operators: "#b0124f"}
	redlineInks = basePalette{text: "#2a1418", comment: "#7a8199", red: "#c8102e", orange: "#c2410c", yellow: "#a35a00",
		green: "#0f7a6c", teal: "#0f7a6c", cyan: "#1d5f80", blue: "#1d5f80", purple: "#b0124f", pink: "#b0124f"}
)

var (
	redlineDark = uiColors{
		background: "#03060f", sidebar: "#060912", statusBase: "#0b0308",
		panel: "#1a0910", block: "#0b1122", body: "#0f172e", button: "#3a1020", tab: "#161d38", done: "#0c1326",
		segment: "#3d0c1a", segmentDim: "#2a0812", segmentDeep: "#1c050c",
		peer: "#151a2a", popup: "#080c18", queued: "#2a1a0e",
		cursorLine: "#2a0a14", blockCursor: "#3a0f1c", vimCursor: "#30101a",
		added: "#062420", removed: "#330a12", addedCursor: "#0c3a33", removedCursor: "#55111f",
		inlineCode: "#ff2a3d", inlineCodeBack: "#240a14", codeBlockBack: "#080e1e",
		text: "#f2dede", subtext: "#d9a3a8", dim: "#8a5a63", faint: "#5c3d4a", empty: "#2a2236", queuedDim: "#a07a52",
		onAccent: "#03060f",
		accent:   "#ff2a3d", warning: "#ffaa33", green: "#3fd9c0", red: "#ff5470", purple: "#ff5a8a", cyan: "#6fb7d8",
		teal: "#3fd9c0", orange: "#ff7a3d", pink: "#ff4fa0",
		uiExtras: uiExtras{
			prompt: "#2c0e18", promptSpeed: "#ff7a8a", generationSpeed: "#ff2a3d",
			separator: "#c8102e", gauge: "#ff2a3d",
			tableHeader: "#a50d26", tableEven: "#2a0812", tableOdd: "#1a050c",
			heading: "#ff2a3d", lineNumber: "#7a1424", lineNumberCursor: "#ff2a3d",
		},
	}
	redlineLight = onPaper("#f6efec", redlineInks, redlineInks.red)
)
