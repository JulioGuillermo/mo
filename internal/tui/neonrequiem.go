package tui

// Neon Requiem is a dead city whose signs still burn: hot pink and violet on
// a night of purple, lavender for what code holds and a cold neon cyan for
// its types.

var (
	neonRequiemNight = machinePalette{text: "#f8e6f6", comment: "#5e4a7e", primary: "#ff2e97", call: "#ffffff",
		strings: "#d6a6ff", numbers: "#e05cff", types: "#7df9ff", operators: "#b44dff"}
	neonRequiemDawn = machinePalette{text: "#2a1028", comment: "#8a7a99", primary: "#c2186f", call: "#1a0a18",
		strings: "#7a2fc0", numbers: "#9b2fc0", types: "#0a7a8a", operators: "#8a1fb0"}
	neonRequiemInks = basePalette{text: "#2a1028", comment: "#8a7a99", red: "#d1243f", orange: "#9b2fc0", yellow: "#a35a00",
		green: "#0a7a8a", teal: "#0a7a8a", cyan: "#0a7a8a", blue: "#7a2fc0", purple: "#7a2fc0", pink: "#c2186f"}
)

var (
	neonRequiemDark = uiColors{
		background: "#07040f", sidebar: "#0b0716", statusBase: "#0e0513",
		panel: "#1d081b", block: "#0e091c", body: "#120b22", button: "#3e0d2d", tab: "#150d28", done: "#0f091e",
		segment: "#400e2e", segmentDim: "#2c0a23", segmentDeep: "#20081d",
		peer: "#130c25", popup: "#0c0718", queued: "#251916",
		cursorLine: "#2c0a23", blockCursor: "#3e0d2d", vimCursor: "#340c27",
		added: "#192933", removed: "#340e1c", addedCursor: "#24414b", removedCursor: "#511526",
		inlineCode: "#ff2e97", inlineCodeBack: "#270921", codeBlockBack: "#0d0819",
		text: "#f8e6f6", subtext: "#faafda", dim: "#8a3666", faint: "#681b53", empty: "#19102f", queuedDim: "#9c6d31",
		onAccent: "#07040f",
		accent:   "#ff2e97", warning: "#ffb347", green: "#7df9ff", red: "#ff3d5a", purple: "#b44dff", cyan: "#7df9ff",
		teal: "#7df9ff", orange: "#e05cff", pink: "#ff79c6",
		uiExtras: uiExtras{
			prompt: "#2f0b25", promptSpeed: "#d68aff", generationSpeed: "#ff2e97",
			separator: "#b0106a", gauge: "#ff2e97",
			tableHeader: "#8e0e58", tableEven: "#2c0a23", tableOdd: "#1d081b",
			heading: "#ff2e97", lineNumber: "#77174c", lineNumberCursor: "#ff2e97",
		},
	}
	neonRequiemLight = onPaper("#f8eef6", neonRequiemInks, neonRequiemInks.pink)
)
