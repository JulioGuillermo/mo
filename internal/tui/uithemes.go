package tui

import (
	"fmt"
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// uiColors are the colours of mo's screen, by what they are for: the
// grounds, from the screen's to the lightest a button has, the text in
// four strengths, and the hues things are told apart by.
type uiColors struct {
	background, sidebar, statusBase string
	// panel is behind what the user wrote and the commands; block behind
	// code, diffs and the shade of dialogs; body, button, tab and done the
	// grounds of dialogs and tables.
	panel, block, body, button, tab, done       string
	segment, segmentDim, segmentDeep            string
	peer, popup, queued                         string
	cursorLine, blockCursor, vimCursor          string
	added, removed, addedCursor, removedCursor  string
	inlineCode, inlineCodeBack, codeBlockBack   string
	text, subtext, dim, faint, empty, queuedDim string
	// onAccent is text on the accent, as a chosen row is.
	onAccent                                                      string
	accent, warning, green, red, purple, cyan, teal, orange, pink string
	uiExtras
}

// uiExtras are the colours a theme may set apart; empty, each takes the
// colour after it: the separators of the status line (faint), the
// context's fill while it has room (green), and a table's header and rows
// (button, body and block). heading colours the headings of markdown, the
// first on the accent, and its links, the words in it and the address
// cyan; empty, they are glamour's own. lineNumber and lineNumberCursor
// colour the numbers of a file's lines, and that of the cursor's; empty,
// they are faint text.
type uiExtras struct {
	separator, gauge                 string
	tableHeader, tableEven, tableOdd string
	heading                          string
	lineNumber, lineNumberCursor     string
}

// uiTheme is a look of the screen for dark backgrounds and one for light.
type uiTheme struct {
	name, about string
	dark, light uiColors
}

func (t *uiTheme) colors(dark bool) uiColors {
	if dark {
		return t.dark
	}
	return t.light
}

var (
	moDark = uiColors{
		background: "#00081a", sidebar: "#060e24", statusBase: "#0b1230",
		panel: "#24283b", block: "#0e1630", body: "#16213f", button: "#26335e", tab: "#1c2850", done: "#101a36",
		segment: "#2a2f4a", segmentDim: "#1f2440", segmentDeep: "#171b33",
		peer: "#1e2030", popup: "#16161e", queued: "#2f2819",
		cursorLine: "#292e42", blockCursor: "#22305a", vimCursor: "#2a3355",
		added: "#12301f", removed: "#3a1622", addedCursor: "#1f4d33", removedCursor: "#5c2535",
		inlineCode: "#7dcfff", inlineCodeBack: "#16264a", codeBlockBack: "#0b1530",
		text: "#c0caf5", subtext: "#a9b1d6", dim: "#737aa2", faint: "#565f89", empty: "#3b4261", queuedDim: "#9a8f78",
		onAccent: "#1a1b26",
		accent:   "#7aa2f7", warning: "#e0af68", green: "#9ece6a", red: "#f7768e", purple: "#bb9af7", cyan: "#7dcfff",
		teal: "#73daca", orange: "#ff9e64", pink: "#ff79c6",
	}
	moLight = uiColors{
		background: "#f7f8fc", sidebar: "#eef1fa", statusBase: "#f4f5fb",
		panel: "#e1e2e7", block: "#e6ecfa", body: "#e1e6f5", button: "#c4cde8", tab: "#d5dcf0", done: "#eef1fa",
		segment: "#d5d9ec", segmentDim: "#e1e4f2", segmentDeep: "#e9ebf6",
		peer: "#e9e9ed", popup: "#f4f5fb", queued: "#f3e5c4",
		cursorLine: "#d8dcea", blockCursor: "#c9d6f2", vimCursor: "#cdd5ee",
		added: "#dcf2dc", removed: "#f8dde2", addedCursor: "#bfe6bf", removedCursor: "#f0c2cb",
		inlineCode: "#2e7de9", inlineCodeBack: "#d5e1f7", codeBlockBack: "#e6ecfa",
		text: "#3760bf", subtext: "#3760bf", dim: "#8990b3", faint: "#8990b3", empty: "#c4c8da", queuedDim: "#7a6a4a",
		onAccent: "#ffffff",
		accent:   "#2e7de9", warning: "#8c6c3e", green: "#587539", red: "#f52a65", purple: "#9854f1", cyan: "#007197",
		teal: "#118c74", orange: "#b15c00", pink: "#d20065",
	}
)

// derived is a look made from a theme's background, text and hues: the
// grounds are the background drawn towards the text, a little more each,
// and the lines of a diff towards green and red.
func derived(background string, p basePalette, accent string, dark bool) uiColors {
	toward := func(target string, share float64) string { return mixHex(background, target, share) }
	ground := func(share float64) string { return toward(p.text, share) }
	onAccent := background
	if !dark {
		onAccent = "#ffffff"
	}
	return uiColors{
		background: background, sidebar: ground(0.03), statusBase: ground(0.05),
		panel: ground(0.12), block: ground(0.05), body: ground(0.09), button: ground(0.18), tab: ground(0.13), done: ground(0.06),
		segment: ground(0.16), segmentDim: ground(0.11), segmentDeep: ground(0.08),
		peer: ground(0.10), popup: ground(0.04), queued: toward(p.yellow, 0.14),
		cursorLine: ground(0.14), blockCursor: toward(accent, 0.18), vimCursor: toward(accent, 0.15),
		added: toward(p.green, 0.15), removed: toward(p.red, 0.15), addedCursor: toward(p.green, 0.28), removedCursor: toward(p.red, 0.28),
		inlineCode: p.cyan, inlineCodeBack: toward(p.blue, 0.16), codeBlockBack: ground(0.04),
		text: p.text, subtext: mixHex(p.text, background, 0.15), dim: mixHex(p.text, background, 0.4), faint: p.comment,
		empty: ground(0.22), queuedDim: mixHex(p.yellow, background, 0.35),
		onAccent: onAccent,
		accent:   accent, warning: p.yellow, green: p.green, red: p.red, purple: p.purple, cyan: p.cyan, teal: p.teal,
		orange: p.orange, pink: p.pink,
	}
}

// uiThemes are the looks of the screen, mo's own first.
var uiThemes = []*uiTheme{
	{name: "mo", about: "mo's own, deep blue", dark: moDark, light: moLight},
	{name: "redline", about: "a machine's display on alert: reds on a night blue", dark: redlineDark, light: redlineLight},
	{name: "tokyo night", about: "Tokyo Night Storm and Day", dark: derived("#24283b", tokyoNight, tokyoNight.blue, true), light: derived("#e1e2e7", tokyoDay, tokyoDay.blue, false)},
	{name: "one", about: "One Dark and One Light", dark: derived("#282c34", oneDark, oneDark.blue, true), light: derived("#fafafa", oneLight, oneLight.blue, false)},
	{name: "catppuccin", about: "Catppuccin Mocha and Latte", dark: derived("#1e1e2e", mocha, mocha.purple, true), light: derived("#eff1f5", latte, latte.purple, false)},
	{name: "material", about: "Material Darker and Lighter", dark: derived("#212121", materialDark, materialDark.blue, true), light: derived("#fafafa", materialLight, materialLight.blue, false)},
	{name: "dracula", about: "Dracula and Alucard", dark: derived("#282a36", dracula, dracula.purple, true), light: derived("#fffbeb", alucard, alucard.purple, false)},
	{name: "gruvbox", about: "Gruvbox dark and light", dark: derived("#282828", gruvboxDark, gruvboxDark.yellow, true), light: derived("#fbf1c7", gruvboxLight, gruvboxLight.blue, false)},
	{name: "nord", about: "Nord, and Snow Storm for light", dark: derived("#2e3440", nordDark, nordDark.cyan, true), light: derived("#eceff4", nordLight, nordLight.blue, false)},
	{name: "solarized", about: "Solarized dark and light", dark: derived("#002b36", solarizedDark, solarizedDark.blue, true), light: derived("#fdf6e3", solarizedLight, solarizedLight.blue, false)},
	{name: "monokai", about: "Monokai and Monokai Pro Light", dark: derived("#272822", monokaiDark, monokaiDark.green, true), light: derived("#faf4f2", monokaiLight, monokaiLight.purple, false)},
}

// uiState is the look in use and whether the screen is dark.
type uiState struct {
	theme *uiTheme
	dark  bool
}

var chosenUI atomic.Pointer[uiState]

func currentUIState() uiState {
	if state := chosenUI.Load(); state != nil {
		return *state
	}
	return uiState{theme: uiThemes[0], dark: true}
}

// ui is the colours of the look in use, for a screen dark or not.
func ui(dark bool) uiColors {
	return currentUIState().theme.colors(dark)
}

// uiNow is the colours in use, the screen's darkness as last set.
func uiNow() uiColors {
	state := currentUIState()
	return state.theme.colors(state.dark)
}

func uiThemeNamed(name string) *uiTheme {
	for _, theme := range uiThemes {
		if theme.name == name {
			return theme
		}
	}
	return uiThemes[0]
}

// useUITheme draws the screen from now on as the look named, dark or
// light; it tells whether that changed anything.
func useUITheme(name string, dark bool) bool {
	next := uiState{theme: uiThemeNamed(name), dark: dark}
	changed := currentUIState() != next
	chosenUI.Store(&next)
	return changed
}

// col is a #rrggbb colour.
func col(hex string) color.Color {
	return lipgloss.Color(hex)
}

// mixHex is a colour share of the way from one to another.
func mixHex(from, to string, share float64) string {
	var r1, g1, b1, r2, g2, b2 int
	fmt.Sscanf(from, "#%02x%02x%02x", &r1, &g1, &b1)
	fmt.Sscanf(to, "#%02x%02x%02x", &r2, &g2, &b2)
	blend := func(a, b int) int { return a + int(float64(b-a)*share+0.5) }
	return fmt.Sprintf("#%02x%02x%02x", blend(r1, r2), blend(g1, g2), blend(b1, b2))
}
