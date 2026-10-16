package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

const (
	// statusPowerline draws the status line as segments on backgrounds,
	// joined by the arrows of powerline fonts; statusPlain as text joined by
	// bars, for fonts without them.
	statusPowerline = "powerline"
	statusPlain     = "plain"
	arrowRight      = "\ue0b0"
	arrowLeft       = "\ue0b2"
	branchIcon      = "\ue0a0"
	iconsNerd       = "nerd"
	iconsText       = "text"
)

// changeIcons are the marks of each kind of change and of a clean tree:
// Nerd Font's Material file icons (file_plus, file_edit, file_minus,
// file_move, file_question, file_alert, file_check), or plain characters.
var changeIcons = map[string]map[rune]string{
	iconsNerd: {
		gitops.ChangeAdded: "\U000f0752", gitops.ChangeModified: "\U000f11e7", gitops.ChangeDeleted: "\U000f1aa1", gitops.ChangeRenamed: "\U000f0ab9",
		gitops.ChangeUntracked: "\U000f086f", gitops.ChangeConflicted: "\U000f0a4b", 0: "\U000f0216",
	},
	iconsText: {
		gitops.ChangeAdded: "+", gitops.ChangeModified: "~", gitops.ChangeDeleted: "-", gitops.ChangeRenamed: "»",
		gitops.ChangeUntracked: "?", gitops.ChangeConflicted: "!", 0: "✓",
	},
}

// icons are the icons in use, Nerd Font's unless set to text.
func (m model) icons() string {
	if m.session.Config.Render.Icons == iconsText {
		return iconsText
	}
	return iconsNerd
}

// segment is a part of the status line: its text in its colours, the
// background it sits on, and what a click and a right click on it do.
// Inner regions, x from the start of the text, take clicks first.
type segment struct {
	text     string
	bg       color.Color
	do, menu func(m model) (tea.Model, tea.Cmd)
	inner    []region
}

// statusParts says which parts of the status line to show.
type statusParts struct {
	hint, prompt, generation, tokens bool
	// provider is 2 for its name, 1 for its first letter, 0 for none.
	provider int
	bar      int
}

// statusTiers are the status lines to try, the fullest first. What goes
// when room is short: the ⇧tab hint, then the provider, shortened to its
// first letter before it goes, then the speeds, the tokens, and the bar
// last, which shrinks to one cell.
var statusTiers = []statusParts{
	{hint: true, provider: 2, prompt: true, generation: true, tokens: true, bar: contextBarCells},
	{provider: 2, prompt: true, generation: true, tokens: true, bar: contextBarCells},
	{provider: 1, prompt: true, generation: true, tokens: true, bar: contextBarCells},
	{prompt: true, generation: true, tokens: true, bar: contextBarCells},
	{generation: true, tokens: true, bar: contextBarCells},
	{tokens: true, bar: contextBarCells},
	{bar: contextBarCells},
	{bar: 5},
	{},
}

// statusLine shows the permission mode, provider and model, how full the
// context is and the last speeds, with the branch and the folder at the
// right end; on a narrow terminal the least needed parts go first.
func (m model) statusLine() string {
	line, _ := m.statusLayout()
	return line
}

// statusLayout is the status line and its clickable parts: the mode goes
// to the next one or lists them all, the provider opens the providers, the
// model lists the models.
func (m model) statusLayout() (string, []region) {
	if m.width <= 0 {
		return m.renderStatus(m.statusSegments(statusTiers[0]), m.suffixSegments(3), 0)
	}
	for _, suffix := range []int{3, 2, 1, 0} {
		right := m.suffixSegments(suffix)
		tiers := statusTiers
		if suffix < 3 {
			// Without room for the branch, nothing that went before it comes back.
			tiers = statusTiers[len(statusTiers)-1:]
		}
		for _, parts := range tiers {
			if line, regions := m.renderStatus(m.statusSegments(parts), right, m.width); ansi.StringWidth(line) <= m.width {
				return line, regions
			}
		}
	}
	line, regions := m.renderStatus(m.statusSegments(statusParts{}), nil, 0)
	return ansi.Truncate(line, m.width, "…"), regions
}

// statusStyle is how the status line is drawn, powerline unless set plain.
func (m model) statusStyle() string {
	if m.session.Config.Render.Status == statusPlain {
		return statusPlain
	}
	return statusPowerline
}

// statusSegments are the parts of the status line on the left.
func (m model) statusSegments(show statusParts) []segment {
	colors := statusPalette(m.printer == nil || m.printer.isDark())
	paint := func(c color.Color, text string) string { return lipgloss.NewStyle().Foreground(c).Render(text) }
	var segments []segment
	if mode := m.vimSegment(); mode.text != "" {
		segments = append(segments, mode)
	}
	if policy := m.session.Agent.Permissions; policy != nil {
		mode := policy.Mode()
		text := lipgloss.NewStyle().Foreground(colors.mode(mode)).Bold(true).Render("● " + string(mode))
		if show.hint {
			text += paint(colors.label, " ⇧tab")
		}
		segments = append(segments, segment{text: text, bg: shade(colors.mode(mode), colors.base, 0.22),
			do: func(m model) (tea.Model, tea.Cmd) {
				return m.setMode(policy.Mode().Next())
			}, menu: func(m model) (tea.Model, tea.Cmd) { return m.modeMenu() }})
	}
	modelName := lipgloss.NewStyle().Foreground(colors.model).Bold(true).Render(shortModelName(m.session.Model))
	if effort := m.currentEffort(); effort != "" {
		modelName += paint(colors.label, " · ") + paint(colors.provider, effort)
	}
	pickModel := func(m model) (tea.Model, tea.Cmd) { return m.runCommand("/model") }
	openProviders := func(m model) (tea.Model, tea.Cmd) { return m.runCommand("/providers") }
	provider := ""
	switch show.provider {
	case 2:
		provider = m.session.Provider
	case 1:
		provider = firstLetter(m.session.Provider)
	}
	if provider != "" {
		provider = paint(colors.provider, provider) + paint(colors.label, " › ")
	}
	width := ansi.StringWidth(provider)
	segments = append(segments, segment{text: provider + modelName, bg: colors.segment, do: pickModel, menu: pickModel,
		inner: []region{{y: 0, x0: 0, x1: width, do: openProviders, menu: openProviders}}})
	if !m.sidebarShown() {
		if context := m.contextPart(show, colors); context != "" {
			segments = append(segments, segment{text: context, bg: colors.segmentDim})
		}
		if speeds := m.speedPart(show, colors); speeds != "" {
			segments = append(segments, segment{text: speeds, bg: colors.segmentDeep})
		}
	}
	if m.flash != "" {
		segments = append(segments, segment{text: lipgloss.NewStyle().Foreground(colors.low).Bold(true).Render("✓ " + m.flash), bg: shade(colors.low, colors.base, 0.18)})
	}
	return segments
}

func firstLetter(name string) string {
	for _, letter := range name {
		return string(letter)
	}
	return ""
}

// suffixSegments are the branch and the folder at the right end: the
// branch with its changes counted (3), without them (2), the folder alone
// (1), or none; the branch whichever it is, also master.
func (m model) suffixSegments(show int) []segment {
	colors := statusPalette(m.printer == nil || m.printer.isDark())
	folder := segment{text: lipgloss.NewStyle().Foreground(colors.text).Render(filepath.Base(m.session.WorkDir)), bg: colors.segmentDim}
	hasBranch := m.branch != ""
	switch {
	case show == 0:
		return nil
	case show == 1 || !hasBranch:
		return []segment{folder}
	}
	icon := branchIcon + " "
	if m.icons() == iconsText {
		icon = ""
	}
	list := func(m model) (tea.Model, tea.Cmd) { return m.showChanges() }
	branch := segment{text: lipgloss.NewStyle().Foreground(colors.branch).Bold(true).Render(icon + m.branch), bg: colors.segment, do: list}
	if show == 3 {
		counts := segment{text: m.changeCounts(colors), bg: colors.segmentDeep, do: list}
		return []segment{counts, branch, folder}
	}
	return []segment{branch, folder}
}

// renderStatus draws the segments on the left and those on the right,
// apart to fill width, with their clickable regions.
func (m model) renderStatus(left, right []segment, width int) (string, []region) {
	var regions []region
	var line strings.Builder
	x := 0
	write := func(text string) {
		line.WriteString(text)
		x += ansi.StringWidth(text)
	}
	place := func(seg segment, start, end int) {
		for _, inner := range seg.inner {
			inner.x0, inner.x1 = inner.x0+start, inner.x1+start
			regions = append(regions, inner)
		}
		if seg.do != nil || seg.menu != nil {
			regions = append(regions, region{y: 0, x0: start, x1: end, do: seg.do, menu: seg.menu})
		}
	}
	plain := m.statusStyle() == statusPlain
	colors := statusPalette(m.printer == nil || m.printer.isDark())
	if plain {
		separator := lipgloss.NewStyle().Foreground(colors.empty).Render(" │ ")
		for i, seg := range left {
			if i > 0 {
				write(separator)
			}
			start := x
			write(seg.text)
			place(seg, start, x)
		}
	} else {
		for i, seg := range left {
			start := x + 1
			write(onBackground(" "+seg.text+" ", seg.bg))
			place(seg, start, x-1)
			arrow := lipgloss.NewStyle().Foreground(seg.bg)
			if i+1 < len(left) {
				arrow = arrow.Background(left[i+1].bg)
			}
			write(arrow.Render(arrowRight))
		}
	}
	var tail strings.Builder
	type span struct{ start, end int }
	spans := make([]span, len(right))
	for i, seg := range right {
		if plain {
			if i > 0 {
				tail.WriteString(lipgloss.NewStyle().Foreground(colors.label).Render(" · "))
			}
			start := ansi.StringWidth(tail.String())
			tail.WriteString(seg.text)
			spans[i] = span{start, ansi.StringWidth(tail.String())}
			continue
		}
		arrow := lipgloss.NewStyle().Foreground(seg.bg)
		if i > 0 {
			arrow = arrow.Background(right[i-1].bg)
		}
		tail.WriteString(arrow.Render(arrowLeft))
		start := ansi.StringWidth(tail.String()) + 1
		tail.WriteString(onBackground(" "+seg.text+" ", seg.bg))
		spans[i] = span{start, ansi.StringWidth(tail.String()) - 1}
	}
	if tail.Len() > 0 {
		gap := max(width-x-ansi.StringWidth(tail.String()), 1)
		write(strings.Repeat(" ", gap))
		offset := x
		write(tail.String())
		for i, seg := range right {
			place(seg, offset+spans[i].start, offset+spans[i].end)
		}
	}
	return line.String(), regions
}

// onBackground puts text, styled with foregrounds only, on a background,
// kept across the resets inside it.
func onBackground(text string, background color.Color) string {
	code := backgroundCode(background)
	text = strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+code)
	text = strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+code)
	return code + text + "\x1b[49m"
}

// shade mixes a colour into a base, by amount of the colour: a background
// that takes the hue of what it holds.
func shade(c, base color.Color, amount float64) color.Color {
	r1, g1, b1, _ := c.RGBA()
	r2, g2, b2, _ := base.RGBA()
	mix := func(a, b uint32) uint8 {
		return uint8((float64(a>>8)*amount + float64(b>>8)*(1-amount)) + 0.5)
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", mix(r1, r2), mix(g1, g2), mix(b1, b2)))
}

// repoMsg is the branch checked out in the working directory and the files
// changed there, read off the UI goroutine.
type repoMsg struct {
	branch  string
	changes []gitops.Change
}

// readRepo reads the branch and the changes when they may have changed: at
// start, after a tool or a turn, and when the terminal gets the focus back,
// never on every redraw. One read at a time: asked while one runs, it runs
// again once that one is back.
func (m *model) readRepo() tea.Cmd {
	if m.repoReading {
		m.repoStale = true
		return nil
	}
	m.repoReading = true
	workDir := m.session.WorkDir
	return func() tea.Msg {
		repo, err := gitops.Open(workDir)
		if err != nil {
			return repoMsg{}
		}
		changes, _ := repo.Changes()
		return repoMsg{branch: repo.CurrentBranch(), changes: changes}
	}
}

// readRepoAgain takes a read that came back, and starts the one asked
// meanwhile.
func (m *model) repoRead(msg repoMsg) tea.Cmd {
	m.branch, m.changes, m.repoReading = msg.branch, msg.changes, false
	if m.repoStale {
		m.repoStale = false
		return m.readRepo()
	}
	return nil
}

// changeCounts are the changes in the working tree as counts by kind, in
// their colours: +2 ~3 -1 ?4, or ✓ when there are none.
func (m model) changeCounts(colors statusColors) string {
	icons := changeIcons[m.icons()]
	space := ""
	if m.icons() == iconsNerd {
		space = " "
	}
	if len(m.changes) == 0 {
		return lipgloss.NewStyle().Foreground(colors.low).Render(icons[0])
	}
	counts := map[rune]int{}
	for _, change := range m.changes {
		counts[change.Kind]++
	}
	tints := map[rune]color.Color{
		gitops.ChangeConflicted: colors.high, gitops.ChangeAdded: colors.low, gitops.ChangeModified: colors.mid,
		gitops.ChangeDeleted: colors.high, gitops.ChangeRenamed: colors.model, gitops.ChangeUntracked: colors.prompt,
	}
	var parts []string
	for _, kind := range []rune{gitops.ChangeConflicted, gitops.ChangeAdded, gitops.ChangeModified, gitops.ChangeDeleted, gitops.ChangeRenamed, gitops.ChangeUntracked} {
		if counts[kind] > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(tints[kind]).Render(fmt.Sprintf("%s%s%d", icons[kind], space, counts[kind])))
		}
	}
	return strings.Join(parts, " ")
}

// showChanges prints the files changed, each with its kind.
func (m model) showChanges() (tea.Model, tea.Cmd) {
	if len(m.changes) == 0 {
		return m.emit(m.printer.Note("no changes on " + orDefault(m.branch, "this branch")))
	}
	lines := []string{fmt.Sprintf("%d changed on %s · + added ~ modified - deleted » renamed ? untracked ! conflict", len(m.changes), m.branch)}
	for _, change := range m.changes {
		lines = append(lines, fmt.Sprintf("%c %s", change.Kind, change.Path))
	}
	return m.emit(m.printer.Note(strings.Join(lines, "\n")))
}

// contextShare is how full the context is, and the colour that says so.
func (m model) contextShare(colors statusColors) (int, int, float64, color.Color) {
	used := m.usage.InputTokens + m.usage.OutputTokens
	if m.running && m.metrics.active() {
		used = m.metrics.context(used)
	}
	window := m.session.Agent.Window
	if window <= 0 {
		return used, 0, 0, colors.low
	}
	share := float64(used) / float64(window)
	switch {
	case share >= 0.8:
		return used, window, share, colors.high
	case share >= 0.5:
		return used, window, share, colors.mid
	}
	return used, window, share, colors.low
}

// contextPart is the context's fill in the status line: a bar, or one cell
// without room for it, the share and the tokens, as much as show asks.
func (m model) contextPart(show statusParts, colors statusColors) string {
	paint := func(c color.Color, text string) string { return lipgloss.NewStyle().Foreground(c).Render(text) }
	used, window, share, fill := m.contextShare(colors)
	switch {
	case window > 0:
		part := paint(fill, contextGauge(share)) + " "
		if show.bar > 0 {
			part = contextBar(share, show.bar, fill, colors.empty) + " "
		}
		part += paint(fill, m.about()+fmt.Sprintf("%.0f%%", share*100))
		if show.tokens {
			part += paint(colors.label, fmt.Sprintf(" %s/%s", formatTokens(used), formatTokens(window)))
		}
		return part
	case used > 0:
		return paint(colors.label, "ctx ") + paint(colors.low, m.about()+formatTokens(used))
	}
	return ""
}

// about marks the context counted from the reply being written, not yet
// the server's exact count.
func (m model) about() string {
	if m.running && m.metrics.active() {
		return "~"
	}
	return ""
}

// speedPart is how fast the last reply read the prompt and wrote, or, while
// one is written, how fast it goes, marked ~.
func (m model) speedPart(show statusParts, colors statusColors) string {
	paint := func(c color.Color, text string) string { return lipgloss.NewStyle().Foreground(c).Render(text) }
	var speeds []string
	prompt, generation := m.speed.PromptRate, m.speed.GenerationRate
	promptLive, generationLive := "", ""
	if m.running {
		if rate := m.metrics.promptRate(time.Now()); rate > 0 && m.metrics.pieces == 0 {
			prompt, promptLive = rate, "~"
		}
		if rate := m.metrics.generation(); rate > 0 {
			generation, generationLive = rate, "~"
		}
	}
	if show.prompt && prompt > 0 {
		speeds = append(speeds, paint(colors.label, "pp ")+paint(colors.prompt, fmt.Sprintf("%s%.0f", promptLive, prompt)))
	}
	if show.generation && generation > 0 {
		speeds = append(speeds, paint(colors.label, "tg ")+paint(colors.generation, fmt.Sprintf("%s%.1f", generationLive, generation)))
	}
	if len(speeds) == 0 {
		return ""
	}
	return strings.Join(speeds, paint(colors.label, " · ")) + paint(colors.label, " t/s")
}
