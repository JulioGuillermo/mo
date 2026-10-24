package tui

// Coldwire is a machine at rest: electric cyan and ice on a deep night
// blue, cobalt for its numbers and periwinkle for its types, amber kept for
// what warns.

var (
	coldwireNight = machinePalette{text: "#e2f4ff", comment: "#4a5a7a", primary: "#00c8ff", call: "#ffffff",
		strings: "#7ff5e0", numbers: "#5d8bff", types: "#b8c7ff", operators: "#3fa9ff"}
	coldwireFrost = machinePalette{text: "#0f2236", comment: "#7a8aa0", primary: "#0077b6", call: "#0a1420",
		strings: "#0a8a72", numbers: "#2f5fd0", types: "#4b5bd6", operators: "#1a6fd0"}
	coldwireInks = basePalette{text: "#0f2236", comment: "#7a8aa0", red: "#d1243f", orange: "#2f5fd0", yellow: "#a35a00",
		green: "#0a8a72", teal: "#0a8a72", cyan: "#0077b6", blue: "#0077b6", purple: "#4b5bd6", pink: "#7a4bd6"}
)

var (
	coldwireDark = uiColors{
		background: "#020812", sidebar: "#050c1a", statusBase: "#020e19",
		panel: "#021927", block: "#070f20", body: "#091327", button: "#023246", tab: "#0c162d", done: "#081022",
		segment: "#023449", segmentDim: "#022536", segmentDeep: "#021b2a",
		peer: "#0a142a", popup: "#050d1b", queued: "#201b16",
		cursorLine: "#022536", blockCursor: "#023246", vimCursor: "#022b3d",
		added: "#0d2d2f", removed: "#301422", addedCursor: "#154642", removedCursor: "#4e1d2c",
		inlineCode: "#00c8ff", inlineCodeBack: "#022131", codeBlockBack: "#060e1d",
		text: "#e2f4ff", subtext: "#9ee7ff", dim: "#237490", faint: "#0a567a", empty: "#0e1a35", queuedDim: "#9a6926",
		onAccent: "#020812",
		accent:   "#00c8ff", warning: "#ffaa33", green: "#4dffd2", red: "#ff4d6a", purple: "#7a8cff", cyan: "#9fd8ff",
		teal: "#4dffd2", orange: "#5d8bff", pink: "#b18cff",
		uiExtras: uiExtras{
			prompt: "#022738", promptSpeed: "#7fe3ff", generationSpeed: "#00c8ff",
			separator: "#0a78b8", gauge: "#00c8ff",
			tableHeader: "#086297", tableEven: "#022536", tableOdd: "#021927",
			heading: "#00c8ff", lineNumber: "#015e7d", lineNumberCursor: "#00c8ff",
		},
	}
	coldwireLight = onPaper("#eef3f8", coldwireInks, coldwireInks.cyan)
)
