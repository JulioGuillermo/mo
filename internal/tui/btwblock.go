package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/sessions"
)

// addBtwBlock adds the block of a side question: folded to a pill that
// says how it goes, opened with a click to its answer between two rules.
func (m *model) addBtwBlock(answer *btwAnswer) {
	printer := m.printer
	b := block{btw: answer, foldable: true, open: true, draw: func(open bool) string { return printer.btw(answer, open) }}
	b.redraw(true)
	m.blocks = append(m.blocks, b)
}

func btwPill() lipgloss.Style {
	return lipgloss.NewStyle().Background(accent()).Foreground(col(uiNow().onAccent)).Bold(true)
}

func btwBar() lipgloss.Style { return lipgloss.NewStyle().Foreground(accent()) }

func btwDetailBackground() color.Color { return col(uiNow().segment) }

// btw draws a side question: a pill that its bar comes out of, with how
// long it is beside it, and opened the question and answer along the bar,
// closed by a rule that says it is not kept.
func (p *printer) btw(answer *btwAnswer, open bool) string {
	p.mu.Lock()
	width, pills := p.width, p.pills
	p.mu.Unlock()
	lines := []string{"", p.btwHeader(answer, open, pills)}
	if !open {
		return strings.Join(lines, "\n")
	}
	for _, line := range p.btwQuestion(answer.question, width) {
		lines = append(lines, "  "+btwBar().Render("│")+" "+line)
	}
	lines = append(lines, "  "+btwBar().Render("│"))
	for _, line := range p.btwBody(answer) {
		lines = append(lines, "  "+btwBar().Render("│")+line)
	}
	lines = append(lines, "  "+btwBar().Render("╰─ ")+dimStyle.Render(btwFooter(answer)))
	return strings.Join(lines, "\n")
}

// btwHeader is the pill, from the bar's column: its mark and name on the
// accent, then how long it is and what a click does on a darker ground,
// closed at both ends as pills says, the end before it in the column left
// of the bar.
func (p *printer) btwHeader(answer *btwAnswer, open bool, pills string) string {
	how := btwLines(answer)
	switch {
	case !answer.done:
		how = "answering…"
	case answer.ending != "" && answer.text == "":
		how = answer.ending
	}
	mark, hint := "▸", "click to open"
	if open {
		mark, hint = "▾", "click to fold"
	}
	drawn := pill(pills, []string{mark + " " + btwLabel(answer) + " ", " " + how + " · " + hint + " "},
		[]color.Color{accent(), btwDetailBackground()}, []color.Color{col(uiNow().onAccent), col(uiNow().subtext)})
	if left, _ := pillEnds(pills); left != "" {
		return " " + drawn
	}
	return "  " + drawn
}

// btwQuestion is the question wrapped to the width, on the ground of the
// user's prompts.
func (p *printer) btwQuestion(question string, width int) []string {
	inner := max(width-6, 10)
	band := panel(p.isDark()).Width(inner)
	if prompt := ui(p.isDark()).prompt; prompt != "" {
		band = band.Background(col(prompt))
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(question), "\n") {
		for _, piece := range strings.Split(ansi.Wrap(line, inner-2, " "), "\n") {
			prefix := "  "
			if len(lines) == 0 {
				prefix = "› "
			}
			lines = append(lines, band.Render(prefix+piece))
		}
	}
	return lines
}

// btwLabel is how its pill names an aside: its word, and its number once
// it has one.
func btwLabel(answer *btwAnswer) string {
	if answer.id == 0 {
		return btwWord(answer)
	}
	return fmt.Sprintf("%s #%d", btwWord(answer), answer.id)
}

// replayAsides adds, in a conversation drawn again, the blocks of the asides
// asked after as many of its messages as from up to to.
func (m *model) replayAsides(from, to int) {
	asides := m.session.Agent.Tools.Asides()
	if asides == nil {
		return
	}
	for _, aside := range asides.All() {
		if aside.After >= from && aside.After < to {
			m.addBtwBlock(&btwAnswer{id: aside.ID, question: aside.Question, bang: aside.Bang, text: aside.Answer,
				done: true, ending: aside.Ending, changes: aside.Changes})
		}
	}
}

// keepAsidesBefore keeps the asides asked after at most count messages,
// for a conversation that goes back to there.
func (m *model) keepAsidesBefore(count int) {
	asides := m.session.Agent.Tools.Asides()
	if asides == nil {
		return
	}
	kept := slices.DeleteFunc(asides.All(), func(aside sessions.Aside) bool { return aside.After > count })
	asides.Restore(kept)
}

// btwBody is the answer drawn as an answer of the model, or while none
// came yet the end of what it reasons.
func (p *printer) btwBody(answer *btwAnswer) []string {
	p.mu.Lock()
	width := p.width
	p.mu.Unlock()
	if answer.drawn != nil && answer.drawnText == answer.text+answer.reasoning && answer.drawnWidth == width {
		return answer.drawn
	}
	answer.drawn, answer.drawnText, answer.drawnWidth = p.drawBtwBody(answer), answer.text+answer.reasoning, width
	return answer.drawn
}

func (p *printer) drawBtwBody(answer *btwAnswer) []string {
	text := strings.TrimSpace(answer.text)
	if text == "" {
		reasoning := strings.Fields(answer.reasoning)
		if len(reasoning) == 0 {
			return []string{dimStyle.Render(" …")}
		}
		said := strings.Join(reasoning[max(len(reasoning)-40, 0):], " ")
		return []string{dimStyle.Render(" " + spark() + " " + said)}
	}
	return strings.Split(strings.Trim(p.Answer(text), "\n"), "\n")
}

// btwFooter says the answer is not kept, how it ended when not well, and
// what /btw! changed.
func btwFooter(answer *btwAnswer) string {
	parts := []string{"not kept in the conversation"}
	if answer.ending != "" {
		parts = append(parts, answer.ending)
	}
	if len(answer.changes) > 0 {
		parts = append(parts, "the model is told it did: "+strings.Join(answer.changes, " · "))
	}
	return strings.Join(parts, " · ")
}

// btwAt is the side question whose block is at row y of the conversation.
func (m model) btwAt(y int) (*btwAnswer, bool) {
	conversation := m.conversation(m.conversationHeight())
	i, found := m.conversationLines().blockAt(y + conversation.YOffset())
	if !found || m.blocks[i].btw == nil {
		return nil, false
	}
	return m.blocks[i].btw, true
}

// btwMenu is what a right click on a side question offers.
func (m model) btwMenu(answer *btwAnswer) (tea.Model, tea.Cmd) {
	options := []huh.Option[string]{huh.NewOption("open in a box", "box"), huh.NewOption("open or fold here", "fold")}
	if strings.TrimSpace(answer.text) != "" {
		options = append(options, huh.NewOption("copy the answer", "copy"))
	}
	return m.showChoices(btwWord(answer), answer.question, options, func(m model, choice string) (tea.Model, tea.Cmd) {
		switch choice {
		case "box":
			return m.openBtwBox(answer)
		case "fold":
			m.toggleBtw(answer)
			return m, nil
		}
		return m.flashCopied(answer.text, "copied the answer")
	})
}

func (m *model) toggleBtw(answer *btwAnswer) {
	m.blocks = slices.Clone(m.blocks)
	for i := range m.blocks {
		if m.blocks[i].btw == answer {
			m.blocks[i].open = !m.blocks[i].open
			m.blocks[i].redraw(m.blocks[i].open)
		}
	}
}
