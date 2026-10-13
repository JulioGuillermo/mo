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
// with tabs to see how many there are and go between them.
type dialog struct {
	interaction agent.Interaction
	reply       chan agent.Reply
	items       []dialogItem
	current     int
}

// dialogItem is one call to approve or one question.
type dialogItem struct {
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
	d := &dialog{interaction: msg.interaction, reply: msg.reply}
	for i := range msg.interaction.Approvals {
		d.items = append(d.items, dialogItem{approval: i, question: -1, decision: agent.Deny})
	}
	for i := range msg.interaction.Questions {
		other := textinput.New()
		other.Placeholder = "your own answer"
		other.Prompt = ""
		d.items = append(d.items, dialogItem{approval: -1, question: i, picked: map[int]bool{}, other: other})
	}
	for i := range d.items {
		if d.items[i].approval >= 0 {
			d.items[i].button = len(d.buttonsFor(d.items[i])) - 1
		}
	}
	return d
}

func (d *dialog) item() *dialogItem {
	return &d.items[d.current]
}

func (d *dialog) request(item dialogItem) agent.ApprovalRequest {
	return d.interaction.Approvals[item.approval]
}

func (d *dialog) questionOf(item dialogItem) tools.Question {
	return d.interaction.Questions[item.question]
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

// openDialog shows what the agent asks, starting at the first part.
func (m model) openDialog(msg interactionMsg) (tea.Model, tea.Cmd) {
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

// next goes to the next part left, or sends every answer once none is.
func (m model) next() (tea.Model, tea.Cmd) {
	d := m.dialog
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
	reply := agent.Reply{Decisions: make([]agent.Decision, len(d.interaction.Approvals)), Answers: make([]string, len(d.interaction.Questions))}
	if !cancelled {
		for _, item := range d.items {
			if item.approval >= 0 {
				reply.Decisions[item.approval] = item.decision
			} else {
				reply.Answers[item.question] = item.answer
			}
		}
	}
	d.reply <- reply
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
	d := m.dialog
	colors := dialogPalette(m.printer == nil || m.printer.isDark())
	width := max(m.width, 30)
	var lines []string
	if len(d.items) > 1 {
		lines = append(lines, m.dialogTabs(colors, width))
	}
	body := lipgloss.NewStyle().Background(colors.body).Foreground(colors.text)
	pad := func(content string) string {
		content = body.Render("  ") + content
		return content + body.Render(strings.Repeat(" ", max(width-ansi.StringWidth(content), 0)))
	}
	lines = append(lines, pad(""))
	item := *d.item()
	var content []string
	if item.approval >= 0 {
		content = m.approvalLines(item, colors, body, width-4)
	} else {
		content = m.questionLines(item, colors, body, width-4)
	}
	for _, line := range content {
		lines = append(lines, pad(line))
	}
	lines = append(lines, pad(""))
	lines = append(lines, dimStyle.Render(m.dialogKeys(item)))
	return strings.Join(lines, "\n")
}

// dialogTabs names every part: the one shown stands out, answered ones are
// ticked.
func (m model) dialogTabs(colors dialogColors, width int) string {
	d := m.dialog
	left := 0
	for _, item := range d.items {
		if !item.answered {
			left++
		}
	}
	var tabs []string
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
		label := mark + name
		tabs = append(tabs, style.Render(label))
	}
	count := dimStyle.Render(fmt.Sprintf(" %d of %d · %d left", d.current+1, len(d.items), left))
	return ansi.Truncate(strings.Join(tabs, " ")+count, width, "…")
}

func (m model) approvalLines(item dialogItem, colors dialogColors, body lipgloss.Style, width int) []string {
	request := m.dialog.request(item)
	title := body.Bold(true).Render("Run " + request.Call.Name + "?")
	if request.Warning != "" {
		title += body.Render("  ") + body.Foreground(lipgloss.Color("#f7768e")).Render(request.Warning)
	}
	lines := []string{title, ""}
	lines = append(lines, m.commandBlock(item, colors, body, width)...)
	parts := m.commandParts(request, body, colors, width)
	lines = append(lines, parts...)
	if request.Reason != "" {
		lines = append(lines, "", body.Foreground(warning).Render(request.Reason))
	}
	if request.Ruled && len(request.Commands) > 0 && len(parts) == 0 {
		lines = append(lines, body.Foreground(colors.dim).Render("no rule allows: ")+body.Render(strings.Join(request.Commands, " · ")))
	}
	var buttons []string
	for i, label := range m.dialog.buttonsFor(item) {
		style := lipgloss.NewStyle().Padding(0, 2).Background(colors.button).Foreground(colors.text)
		if i == item.button {
			style = style.Background(colors.chosen).Foreground(colors.chosenText).Bold(true)
		}
		buttons = append(buttons, style.Render(label))
	}
	return append(lines, "", strings.Join(buttons, body.Render("  ")))
}

// commandRows is how many lines of a command the dialog shows before it
// scrolls: about half the screen, less what surrounds it.
func (m model) commandRows() int {
	if m.height <= 0 {
		return 12
	}
	return min(max(m.height/2-8, 4), 40)
}

// commandLines is the command of a call highlighted and wrapped to width.
func (m model) commandLines(request agent.ApprovalRequest, colors dialogColors, width int) []string {
	language := ""
	if request.Call.Name == "bash" {
		language = "bash"
	}
	return highlightLines(request.Summary, language, width-2, colors.command, m.printer == nil || m.printer.isDark())
}

// commandBlock shows the whole command, wrapped, scrolled to where the
// user moved it when it does not fit.
func (m model) commandBlock(item dialogItem, colors dialogColors, body lipgloss.Style, width int) []string {
	command := lipgloss.NewStyle().Background(colors.command)
	all := m.commandLines(m.dialog.request(item), colors, width)
	rows := m.commandRows()
	first := min(max(item.scroll, 0), max(len(all)-rows, 0))
	shown := all[first:min(first+rows, len(all))]
	var lines []string
	for _, line := range shown {
		lines = append(lines, command.Render(" ")+line+command.Render(strings.Repeat(" ", max(width-1-ansi.StringWidth(line), 0))))
	}
	if len(all) > rows {
		lines = append(lines, body.Foreground(colors.dim).Render(fmt.Sprintf("lines %d–%d of %d · ↑↓ pgup pgdn scroll", first+1, first+len(shown), len(all))))
	}
	return lines
}

// scrollCommand moves the command shown by a line, a page or to an end.
func (m model) scrollCommand(item *dialogItem, pressed string) {
	total := len(m.commandLines(m.dialog.request(*item), dialogPalette(true), max(m.width, 30)-4))
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

const shownParts = 8

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
	for i, part := range parts {
		if i == shownParts {
			lines = append(lines, body.Foreground(colors.dim).Render(fmt.Sprintf("  … +%d more", len(parts)-shownParts)))
			break
		}
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
		text := oneLine(part.Text, max(width-ansi.StringWidth(note)-8, 12))
		lines = append(lines, body.Foreground(tint).Render("  "+mark+" ")+body.Render(text)+body.Foreground(colors.dim).Render("  "+note))
	}
	return lines
}

func (m model) questionLines(item dialogItem, colors dialogColors, body lipgloss.Style, width int) []string {
	question := m.dialog.questionOf(item)
	var lines []string
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
		lines = append(lines, style.Render(mark+oneLine(option, width-8)))
	}
	if item.cursor == len(question.Options) {
		item.other.SetWidth(max(width-8, 10))
		lines = append(lines, chosen.Render("❯ ✎ ")+item.other.View())
	} else {
		lines = append(lines, body.Foreground(colors.dim).Render("  ✎ your own answer"))
	}
	return lines
}

func (m model) dialogKeys(item dialogItem) string {
	keys := []string{}
	if item.approval >= 0 {
		keys = append(keys, "←→ choose · enter press · y yes · n no")
		if len(m.commandLines(m.dialog.request(item), dialogPalette(true), max(m.width, 30)-4)) > m.commandRows() {
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
				return b.printer.ToolStart(request.Call.Name, request.Summary) + "\n" + b.printer.Denied(request.Call.Name)
			})
		}
	}
	return answered
}
