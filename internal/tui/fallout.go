package tui

// Fallout is a machine in the wasteland: a radiation green on a night of
// dark teal, orange of rust and fire for what code holds and its numbers,
// lime for its operators.

var (
	falloutNight = machinePalette{text: "#e4ffe9", comment: "#4f6a5a", primary: "#3dff7a", call: "#ffffff",
		strings: "#ffa040", numbers: "#ff7a1a", types: "#6fd8c8", operators: "#c8ff4d"}
	falloutDust = machinePalette{text: "#14261a", comment: "#7f8c80", primary: "#16803a", call: "#0a140c",
		strings: "#c2540a", numbers: "#a3400a", types: "#1a6a7a", operators: "#5a7a00"}
	falloutInks = basePalette{text: "#14261a", comment: "#7f8c80", red: "#c4301f", orange: "#c2540a", yellow: "#a35a00",
		green: "#16803a", teal: "#1a7a6c", cyan: "#1a6a7a", blue: "#1a6a7a", purple: "#5a7a00", pink: "#c2540a"}
)

var (
	falloutDark = uiColors{
		background: "#030a0a", sidebar: "#050f0f", statusBase: "#05110d",
		panel: "#082014", block: "#071313", body: "#081616", button: "#104023", tab: "#0a1a1a", done: "#071414",
		segment: "#104224", segmentDim: "#0c2f1b", segmentDeep: "#092215",
		peer: "#091818", popup: "#051010", queued: "#211b0e",
		cursorLine: "#0c2f1b", blockCursor: "#104023", vimCursor: "#0d361e",
		added: "#0c2f1b", removed: "#301613", addedCursor: "#124726", removedCursor: "#4f1e19",
		inlineCode: "#3dff7a", inlineCodeBack: "#0b2a19", codeBlockBack: "#061111",
		text: "#e4ffe9", subtext: "#b2ffc8", dim: "#3b8c56", faint: "#1c6c3f", empty: "#0c1f1f", queuedDim: "#9a601d",
		onAccent: "#030a0a",
		accent:   "#3dff7a", warning: "#ff9a2a", green: "#3dff7a", red: "#ff4d3d", purple: "#ffa040", cyan: "#6fd8c8",
		teal: "#3dff7a", orange: "#ff7a1a", pink: "#ffb86c",
		uiExtras: uiExtras{
			prompt: "#0c311c", promptSpeed: "#ffa040", generationSpeed: "#3dff7a",
			separator: "#139a45", gauge: "#3dff7a",
			tableHeader: "#107d39", tableEven: "#0c2f1b", tableOdd: "#082014",
			heading: "#3dff7a", lineNumber: "#1d783c", lineNumberCursor: "#3dff7a",
		},
	}

	falloutLight = onPaper("#f1f4ec", falloutInks, falloutInks.green)
)
