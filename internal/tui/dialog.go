package tui

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

const (
	tabLength = 24
)

// interactionMsg carries what the calls of a reply need from the user.
type interactionMsg struct {
	interaction agent.Interaction
	reply       chan agent.Reply
}

// dialog shows what the agent asks, pinned above the input until every
// part is answered: the calls to approve and the questions, one at a time,
// with tabs to see how many there are and go between them. What a call
// written later asks joins it while it is open; each group of parts is
// answered as soon as all of its are.
type dialog struct {
	groups  []*dialogGroup
	items   []dialogItem
	current int
}

// dialogGroup is what one ask of the agent's needs, answered on its own.
type dialogGroup struct {
	interaction agent.Interaction
	reply       chan agent.Reply
	sent        bool
}

// dialogItem is one call to approve or one question, of its group.
type dialogItem struct {
	group    int
	approval int
	question int
	answered bool

	decision agent.Decision
	button   int
	scroll   int

	answer string
	cursor int
	picked map[int]bool
	other  textinput.Model
}

type dialogButton struct {
	label  string
	key    string
	action func(m model) (tea.Model, tea.Cmd)
}

func newDialog(msg interactionMsg) *dialog {
	d := &dialog{}
	d.add(msg)
	return d
}

// add puts what an ask needs after the parts already shown.
func (d *dialog) add(msg interactionMsg) {
	group := len(d.groups)
	d.groups = append(d.groups, &dialogGroup{interaction: msg.interaction, reply: msg.reply})
	first := len(d.items)
	for i := range msg.interaction.Approvals {
		d.items = append(d.items, dialogItem{group: group, approval: i, question: -1, decision: agent.Deny})
	}
	for i := range msg.interaction.Questions {
		other := textinput.New()
		other.Placeholder = "your own answer"
		other.Prompt = ""
		d.items = append(d.items, dialogItem{group: group, approval: -1, question: i, picked: map[int]bool{}, other: other})
	}
	for i := first; i < len(d.items); i++ {
		if d.items[i].approval >= 0 {
			d.items[i].button = len(d.buttonsFor(d.items[i])) - 1
		}
	}
	d.sendAnswered()
}

func (d *dialog) item() *dialogItem {
	return &d.items[d.current]
}

func (d *dialog) request(item dialogItem) agent.ApprovalRequest {
	return d.groups[item.group].interaction.Approvals[item.approval]
}

func (d *dialog) questionOf(item dialogItem) tools.Question {
	return d.groups[item.group].interaction.Questions[item.question]
}

// sendAnswered answers every group whose parts are all answered, or all
// of them, as denied, when cancelled.
func (d *dialog) sendAnswered(cancelled ...bool) {
	stop := len(cancelled) > 0 && cancelled[0]
	for g, group := range d.groups {
		if group.sent {
			continue
		}
		reply := agent.Reply{Decisions: make([]agent.Decision, len(group.interaction.Approvals)), Answers: make([]string, len(group.interaction.Questions))}
		complete := true
		for _, item := range d.items {
			if item.group != g {
				continue
			}
			complete = complete && item.answered
			if item.approval >= 0 {
				reply.Decisions[item.approval] = item.decision
			} else {
				reply.Answers[item.question] = item.answer
			}
		}
		switch {
		case stop:
			reply = agent.Reply{Decisions: make([]agent.Decision, len(group.interaction.Approvals)), Answers: make([]string, len(group.interaction.Questions))}
		case !complete:
			continue
		}
		group.reply <- reply
		group.sent = true
	}
}

// buttonsFor are the answers to a call: yes, always when it can be, no. No
// is last and starts selected, so enter alone never lets a call through.
func (d *dialog) buttonsFor(item dialogItem) []string {
	request := d.request(item)
	switch {
	case request.Warning != "":
		return []string{"Yes", "No"}
	case request.Ruled && len(request.Commands) > 0:
		return []string{"Yes", "Always… add a rule", "No"}
	case request.Ruled:
		return []string{"Yes", "No"}
	}
	return []string{"Yes", "Always this session", "No"}
}

// openDialog shows what the agent asks, starting at the first part; with
// a dialog open, what is asked joins it.
func (m model) openDialog(msg interactionMsg) (tea.Model, tea.Cmd) {
	if m.dialog != nil {
		m.dialog.add(msg)
		return m, m.interactionAsked(msg.interaction)
	}
	m.dialog = newDialog(msg)
	if len(m.dialog.items) == 0 {
		msg.reply <- agent.Reply{}
		m.dialog = nil
		return m, nil
	}
	return m, tea.Batch(m.focusItem(), m.interactionAsked(msg.interaction))
}

// focusItem puts the cursor in the answer box of a question that has one
// selected.
func (m *model) focusItem() tea.Cmd {
	item := m.dialog.item()
	if item.question < 0 {
		return nil
	}
	if item.cursor == len(m.dialog.questionOf(*item).Options) {
		return item.other.Focus()
	}
	item.other.Blur()
	return nil
}

// handleDialogKey answers the part shown, or moves between parts.
func (m model) handleDialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.dialog
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeDialog(true)
		m.stop()
		return m, nil
	case "tab":
		d.current = (d.current + 1) % len(d.items)
		return m, m.focusItem()
	case "shift+tab":
		d.current = (d.current + len(d.items) - 1) % len(d.items)
		return m, m.focusItem()
	}
	if d.item().approval >= 0 {
		return m.handleApprovalKey(msg.String())
	}
	return m.handleQuestionKey(msg)
}

func (m model) handleApprovalKey(pressed string) (tea.Model, tea.Cmd) {
	d := m.dialog
	item := d.item()
	buttons := d.buttonsFor(*item)
	switch pressed {
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		m.scrollCommand(item, pressed)
		return m, nil
	case "left", "h":
		item.button = max(item.button-1, 0)
		return m, nil
	case "right", "l":
		item.button = min(item.button+1, len(buttons)-1)
		return m, nil
	case "enter", "space":
		pressed = strings.ToLower(buttons[item.button][:1])
	}
	switch pressed {
	case "y":
		return m.decide(agent.Allow)
	case "n":
		return m.decide(agent.Deny)
	case "a":
		if len(buttons) < 3 {
			return m, nil
		}
		if d.request(*item).Ruled {
			return m.editRules(d.current)
		}
		return m.decide(agent.AllowAlways)
	}
	return m, nil
}

func (m model) handleQuestionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.dialog
	item := d.item()
	question := d.questionOf(*item)
	onOther := item.cursor == len(question.Options)
	switch msg.String() {
	case "up":
		item.cursor = max(item.cursor-1, 0)
		return m, m.focusItem()
	case "down":
		item.cursor = min(item.cursor+1, len(question.Options))
		return m, m.focusItem()
	case "space":
		if !onOther && question.Multiple {
			item.picked[item.cursor] = !item.picked[item.cursor]
			return m, nil
		}
	case "enter":
		return m.answer(m.chosenAnswer(*item, question))
	}
	if !onOther {
		return m, nil
	}
	var cmd tea.Cmd
	item.other, cmd = item.other.Update(msg)
	return m, cmd
}

// chosenAnswer is what enter answers: the text typed, the options ticked,
// or the option under the cursor.
// pasteIntoDialog puts pasted text into the answer box of the question shown.
func (m model) pasteIntoDialog(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	item := m.dialog.item()
	if item.question < 0 || item.cursor != len(m.dialog.questionOf(*item).Options) {
		return m, nil
	}
	var cmd tea.Cmd
	item.other, cmd = item.other.Update(msg)
	return m, cmd
}

func (m model) chosenAnswer(item dialogItem, question tools.Question) string {
	if item.cursor == len(question.Options) {
		return strings.TrimSpace(item.other.Value())
	}
	if !question.Multiple {
		return question.Options[item.cursor]
	}
	var picked []string
	for i, option := range question.Options {
		if item.picked[i] || (i == item.cursor && len(item.picked) == 0) {
			picked = append(picked, option)
		}
	}
	return strings.Join(picked, ", ")
}

// decide answers the call shown. Allowing a tool for the session also
// allows the other calls to it waiting here.
func (m model) decide(decision agent.Decision) (tea.Model, tea.Cmd) {
	d := m.dialog
	item := d.item()
	item.decision, item.answered = decision, true
	if decision == agent.AllowAlways {
		name := d.request(*item).Call.Name
		for i := range d.items {
			other := &d.items[i]
			if other.approval >= 0 && !other.answered && d.request(*other).Call.Name == name && d.request(*other).Warning == "" && !d.request(*other).Ruled {
				other.decision, other.answered = agent.Allow, true
			}
		}
	}
	return m.next()
}

func (m model) answer(text string) (tea.Model, tea.Cmd) {
	item := m.dialog.item()
	item.answer, item.answered = text, true
	return m.next()
}

// allowByRules lets through the calls waiting here that new rules now
// cover in every part.
func (m *model) allowByRules(policy *permissions.Policy) {
	d := m.dialog
	for i := range d.items {
		item := &d.items[i]
		if item.approval < 0 || item.answered {
			continue
		}
		request := d.request(*item)
		if !request.Ruled || request.Warning != "" || len(request.Commands) == 0 {
			continue
		}
		covered := true
		for _, command := range request.Commands {
			if verdict, _ := policy.Check(command); verdict != permissions.Allowed {
				covered = false
			}
		}
		if covered {
			item.decision, item.answered = agent.Allow, true
		}
	}
}

// next answers the groups now complete, so their calls run, and goes to
// the next part left; with none, the dialog closes.
func (m model) next() (tea.Model, tea.Cmd) {
	d := m.dialog
	d.sendAnswered()
	for step := 1; step <= len(d.items); step++ {
		i := (d.current + step) % len(d.items)
		if !d.items[i].answered {
			d.current = i
			return m, m.focusItem()
		}
	}
	m.closeDialog(false)
	return m, nil
}

// closeDialog sends the answers, or denies everything when cancelled.
func (m *model) closeDialog(cancelled bool) {
	d := m.dialog
	if d == nil {
		return
	}
	d.sendAnswered(cancelled)
	m.dialog = nil
}

// dialogColors are the blues of the dialog: its body, the command in it,
// the buttons and the tabs.
type dialogColors struct {
	body, command, button, chosen, chosenText, tab, done, text, dim color.Color
}

func dialogPalette(dark bool) dialogColors {
	if dark {
		return dialogColors{
			body: lipgloss.Color("#16213f"), command: lipgloss.Color("#0e1630"), button: lipgloss.Color("#26335e"),
			chosen: accent, chosenText: lipgloss.Color("#0b1020"), tab: lipgloss.Color("#1c2850"), done: lipgloss.Color("#101a36"),
			text: lipgloss.Color("#c0caf5"), dim: lipgloss.Color("#737aa2"),
		}
	}
	return dialogColors{
		body: lipgloss.Color("#e1e6f5"), command: lipgloss.Color("#d0d8ef"), button: lipgloss.Color("#c4cde8"),
		chosen: lipgloss.Color("#2e7de9"), chosenText: lipgloss.Color("#ffffff"), tab: lipgloss.Color("#d5dcf0"), done: lipgloss.Color("#eef1fa"),
		text: lipgloss.Color("#3760bf"), dim: lipgloss.Color("#8990b3"),
	}
}

// dialogView draws the tabs, when there is more than one part, the part
// shown on its own background, and the keys.
func (m model) dialogView() string {
	view, _ := m.dialogLayout()
	return view
}

// dialogLayout draws the dialog and says what its tabs, buttons and options
// do when clicked.
func (m model) dialogLayout() (string, []region) {
	d := m.dialog
	colors := dialogPalette(m.printer == nil || m.printer.isDark())
	width := max(m.mainWidth(), 30)
	var lines []string
	var regions []region
	if len(d.items) > 1 {
		tabs, tabRegions := m.dialogTabs(colors, width)
		lines = append(lines, tabs)
		regions = append(regions, tabRegions...)
	}
	body := lipgloss.NewStyle().Background(colors.body).Foreground(colors.text)
	pad := func(content string) string {
		content = body.Render("  ") + content
		return content + body.Render(strings.Repeat(" ", max(width-ansi.StringWidth(content), 0)))
	}
	item := *d.item()
	if from := d.groups[item.group].interaction.From; from != "" {
		lines = append(lines, pad(lipgloss.NewStyle().Background(colors.body).Foreground(lipgloss.Color("#e0af68")).Bold(true).Render("? asked by subagent "+from)))
	} else {
		lines = append(lines, pad(""))
	}
	var content []string
	var contentRegions []region
	if item.approval >= 0 {
		content, contentRegions = m.approvalLines(item, colors, body, width-4)
	} else {
		content, contentRegions = m.questionLines(item, colors, body, width-4)
	}
	for _, r := range shifted(contentRegions, len(lines)) {
		r.x0, r.x1 = r.x0+2, r.x1+2
		regions = append(regions, r)
	}
	for _, line := range content {
		lines = append(lines, pad(line))
	}
	lines = append(lines, pad(""))
	lines = append(lines, dimStyle.Render(m.dialogKeys(item)))
	return strings.Join(lines, "\n"), regions
}

// dialogTabs names every part: the one shown stands out, answered ones are
// ticked.
func (m model) dialogTabs(colors dialogColors, width int) (string, []region) {
	d := m.dialog
	left := 0
	for _, item := range d.items {
		if !item.answered {
			left++
		}
	}
	var tabs []string
	var regions []region
	x := 0
	for i, item := range d.items {
		mark, name := "⚑ ", ""
		if item.question >= 0 {
			mark, name = "? ", oneLine(d.questionOf(item).Text, tabLength)
		} else {
			name = d.request(item).Call.Name
		}
		style := lipgloss.NewStyle().Padding(0, 1).Background(colors.tab).Foreground(colors.text)
		switch {
		case i == d.current:
			style = style.Background(colors.chosen).Foreground(colors.chosenText).Bold(true)
		case item.answered:
			mark = "✓ "
			style = style.Background(colors.done).Foreground(colors.dim)
		}
		tab := style.Render(mark + name)
		index := i
		regions = append(regions, region{y: 0, x0: x, x1: x + ansi.StringWidth(tab), do: func(m model) (tea.Model, tea.Cmd) {
			m.dialog.current = index
			return m, m.focusItem()
		}})
		x += ansi.StringWidth(tab) + 1
		tabs = append(tabs, tab)
	}
	count := dimStyle.Render(fmt.Sprintf(" %d of %d · %d left", d.current+1, len(d.items), left))
	return ansi.Truncate(strings.Join(tabs, " ")+count, width, "…"), regions
}

func (m model) approvalLines(item dialogItem, colors dialogColors, body lipgloss.Style, width int) ([]string, []region) {
	request := m.dialog.request(item)
	title := body.Bold(true).Render("Run " + request.Call.Name + "?")
	if request.Warning != "" {
		title += body.Render("  ") + body.Foreground(lipgloss.Color("#f7768e")).Render(request.Warning)
	}
	lines := []string{title, ""}
	lines = append(lines, m.approvalBody(item, colors, body, width)...)
	lines = append(lines, "")
	var buttons []string
	var regions []region
	x := 0
	for i, label := range m.dialog.buttonsFor(item) {
		button := buttonLabel(label, i == item.button, colors)
		key := strings.ToLower(label[:1])
		index := i
		regions = append(regions, region{y: len(lines), x0: x, x1: x + ansi.StringWidth(button), do: func(m model) (tea.Model, tea.Cmd) {
			m.dialog.item().button = index
			return m.handleApprovalKey(key)
		}})
		x += ansi.StringWidth(button) + 2
		buttons = append(buttons, button)
	}
	return append(lines, strings.Join(buttons, body.Render("  "))), regions
}

// buttonLabel draws a button with the key that presses it underlined, in
// the warning colour unless the button is the chosen one.
func buttonLabel(label string, chosen bool, colors dialogColors) string {
	style := lipgloss.NewStyle().Background(colors.button).Foreground(colors.text)
	key := style.Foreground(warning).Bold(true).Underline(true)
	if chosen {
		style = style.Background(colors.chosen).Foreground(colors.chosenText).Bold(true)
		key = style.Underline(true)
	}
	return style.Render("  ") + key.Render(label[:1]) + style.Render(label[1:]+"  ")
}

// commandRows is how many lines of a call's command and its parts the
// dialog shows before they scroll: the screen less the title, buttons and
// keys around them, the input and the status line under it.
func (m model) commandRows() int {
	if m.height <= 0 {
		return 12
	}
	return min(max(m.height-16, 6), 80)
}

// commandLines is the command of a call highlighted and wrapped to width.
func (m model) commandLines(request agent.ApprovalRequest, colors dialogColors, width int) []string {
	language := ""
	if request.Call.Name == "bash" {
		language = "bash"
	}
	return highlightLines(request.Summary, language, width-2, colors.command, m.printer == nil || m.printer.isDark())
}

// approvalLinesAll is what a call to approve shows under its title: its
// command, wrapped, the parts of a bash command and what the rules say of
// each, and why it asks; all of it, to scroll.
func (m model) approvalLinesAll(request agent.ApprovalRequest, colors dialogColors, body lipgloss.Style, width int) []string {
	command := lipgloss.NewStyle().Background(colors.command)
	var lines []string
	for _, line := range m.commandLines(request, colors, width) {
		lines = append(lines, command.Render(" ")+line+command.Render(strings.Repeat(" ", max(width-1-ansi.StringWidth(line), 0))))
	}
	parts := m.commandParts(request, body, colors, width)
	lines = append(lines, parts...)
	if request.Reason != "" {
		lines = append(lines, "")
		for _, line := range strings.Split(ansi.Wrap(request.Reason, width, " "), "\n") {
			lines = append(lines, body.Foreground(warning).Render(line))
		}
	}
	if request.Ruled && len(request.Commands) > 0 && len(parts) == 0 {
		for i, line := range strings.Split(ansi.Wrap("no rule allows: "+strings.Join(request.Commands, " · "), width, " "), "\n") {
			if i == 0 {
				line = body.Foreground(colors.dim).Render("no rule allows: ") + body.Render(strings.TrimPrefix(line, "no rule allows: "))
			} else {
				line = body.Render(line)
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// approvalBody shows what a call to approve shows, scrolled to where the
// user moved it when it does not fit, the command and its parts as one.
func (m model) approvalBody(item dialogItem, colors dialogColors, body lipgloss.Style, width int) []string {
	all := m.approvalLinesAll(m.dialog.request(item), colors, body, width)
	rows := m.commandRows()
	if len(all) <= rows {
		return all
	}
	first := min(max(item.scroll, 0), max(len(all)-rows, 0))
	shown := append([]string{}, all[first:min(first+rows, len(all))]...)
	return append(shown, body.Foreground(colors.dim).Render(fmt.Sprintf("lines %d–%d of %d · ↑↓ pgup pgdn scroll", first+1, first+rows, len(all))))
}

// approvalTotal is how many lines a call to approve has to scroll through.
func (m model) approvalTotal(item dialogItem) int {
	colors := dialogPalette(m.printer == nil || m.printer.isDark())
	body := lipgloss.NewStyle().Background(colors.body).Foreground(colors.text)
	return len(m.approvalLinesAll(m.dialog.request(item), colors, body, max(m.mainWidth(), 30)-4-4))
}

// scrollCommand moves what a call to approve shows by a line, a page or to
// an end.
func (m model) scrollCommand(item *dialogItem, pressed string) {
	total := m.approvalTotal(*item)
	rows := m.commandRows()
	last := max(total-rows, 0)
	item.scroll = min(max(item.scroll, 0), last)
	switch pressed {
	case "up", "k":
		item.scroll--
	case "down", "j":
		item.scroll++
	case "pgup":
		item.scroll -= rows
	case "pgdown":
		item.scroll += rows
	case "home":
		item.scroll = 0
	case "end":
		item.scroll = last
	}
	item.scroll = min(max(item.scroll, 0), last)
}

// commandParts lists the simple commands of a bash call and what the
// rules say of each: allowed and by which rule, denied, not covered yet,
// or writing a file.
func (m model) commandParts(request agent.ApprovalRequest, body lipgloss.Style, colors dialogColors, width int) []string {
	if request.Call.Name != "bash" {
		return nil
	}
	parts, err := permissions.Commands(request.Summary)
	if err != nil {
		return []string{"", body.Foreground(warning).Render("could not split it into parts: " + oneLine(err.Error(), width-30))}
	}
	if len(parts) < 2 && !slices.ContainsFunc(parts, func(part permissions.Command) bool { return part.Writes }) {
		return nil
	}
	policy := m.session.Agent.Permissions
	lines := []string{"", body.Foreground(colors.dim).Render(fmt.Sprintf("%d parts", len(parts)))}
	for _, part := range parts {
		mark, note, tint := "●", "no rule allows it", warning
		if policy != nil {
			switch verdict, rule := policy.Check(part.Text); verdict {
			case permissions.Allowed:
				mark, note, tint = "✓", "allowed: "+rule, lipgloss.Color("#9ece6a")
			case permissions.Denied:
				mark, note, tint = "✗", "deny rule: "+rule, lipgloss.Color("#f7768e")
			}
		}
		if part.Writes {
			mark, note = "✎", note+" · writes a file"
		}
		shown := orDefault(part.Source, part.Text)
		for j, line := range highlightLines(shown, "bash", max(width-4, 12), colors.body, m.printer == nil || m.printer.isDark()) {
			lead := "    "
			if j == 0 {
				lead = "  " + mark + " "
			}
			lines = append(lines, body.Foreground(tint).Render(lead)+line)
		}
		lines = append(lines, body.Foreground(colors.dim).Render("    "+oneLine(note, max(width-4, 12))))
	}
	return lines
}

func (m model) questionLines(item dialogItem, colors dialogColors, body lipgloss.Style, width int) ([]string, []region) {
	question := m.dialog.questionOf(item)
	var lines []string
	var regions []region
	for _, line := range strings.Split(ansi.Wrap(question.Text, width, ""), "\n") {
		lines = append(lines, body.Bold(true).Render(line))
	}
	lines = append(lines, "")
	chosen := body.Foreground(colors.chosen).Bold(true)
	for i, option := range question.Options {
		mark := "  "
		if i == item.cursor {
			mark = "❯ "
		}
		if question.Multiple {
			box := "[ ] "
			if item.picked[i] {
				box = "[x] "
			}
			mark += box
		}
		style := body
		if i == item.cursor {
			style = chosen
		}
		index := i
		regions = append(regions, row(len(lines), func(m model) (tea.Model, tea.Cmd) {
			item := m.dialog.item()
			item.cursor = index
			if m.dialog.questionOf(*item).Multiple {
				item.picked[index] = !item.picked[index]
				return m, m.focusItem()
			}
			return m.answer(m.dialog.questionOf(*item).Options[index])
		}))
		lines = append(lines, style.Render(mark+oneLine(option, width-8)))
	}
	regions = append(regions, row(len(lines), func(m model) (tea.Model, tea.Cmd) {
		item := m.dialog.item()
		item.cursor = len(m.dialog.questionOf(*item).Options)
		return m, m.focusItem()
	}))
	if item.cursor == len(question.Options) {
		item.other.SetWidth(max(width-8, 10))
		lines = append(lines, chosen.Render("❯ ✎ ")+item.other.View())
	} else {
		lines = append(lines, body.Foreground(colors.dim).Render("  ✎ your own answer"))
	}
	return lines, regions
}

func (m model) dialogKeys(item dialogItem) string {
	keys := []string{}
	if item.approval >= 0 {
		keys = append(keys, "←→ choose · enter press · y yes · n no")
		if m.approvalTotal(item) > m.commandRows() {
			keys = append(keys, "↑↓ scroll")
		}
		if len(m.dialog.buttonsFor(item)) == 3 {
			keys = append(keys, "a always")
		}
	} else {
		keys = append(keys, "↑↓ choose · enter answer")
		if m.dialog.questionOf(item).Multiple {
			keys = append(keys, "space tick")
		}
		keys = append(keys, "empty answer skips")
	}
	if len(m.dialog.items) > 1 {
		keys = append(keys, "tab next")
	}
	return "  " + strings.Join(append(keys, "esc deny all and stop"), " · ")
}

// interact shows what a reply's calls need and waits for the answers. The
// calls denied are printed, since they never start.
func (b *bridge) interact(ctx context.Context, interaction agent.Interaction) agent.Reply {
	reply := make(chan agent.Reply, 1)
	b.program.Send(interactionMsg{interaction: interaction, reply: reply})
	var answered agent.Reply
	select {
	case answered = <-reply:
	case <-ctx.Done():
		return agent.Reply{}
	}
	for i, request := range interaction.Approvals {
		if i < len(answered.Decisions) && answered.Decisions[i] == agent.Deny {
			b.draw(func() string {
				denied := b.printer.ToolDenied(request.Call.Name, request.Call.Args, request.Summary) + "\n" + b.printer.Denied(request.Call.Name)
				if interaction.From != "" {
					denied = b.printer.Note("subagent "+interaction.From+":") + "\n" + denied
				}
				return denied
			})
		}
	}
	return answered
}
