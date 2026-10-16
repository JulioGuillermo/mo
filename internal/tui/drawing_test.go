package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | WD-01 | Happy    | A diagram wider than the conversation says so under it; one that fits says nothing | P1 |
// | WD-02 | Happy    | A click on a wide diagram opens it whole, scrolled across with ←→, its colours kept | P1 |
// | WD-03 | Happy    | Dragging pans the diagram, without copying; shift and the wheel move it across | P1   |
// | WD-05 | Happy    | A right click on a diagram offers its code: shown in its place and back, copied; s does so whole | P1 |
// | WD-06 | Edge     | A menu option clicked opens nothing under it; ctrl+↑↓ and, with nothing typed, ctrl+hjkl scroll and pan; /diagrams switches all | P1 |
// | WD-07 | Happy    | A right click on an SVG shows its code in its place and the picture back, or copies it | P1 |
// | WD-08 | Happy    | With kitty images, an SVG written or a .svg file shown swaps its picture for its code and back | P1 |
// | WD-04 | Happy    | ctrl+←→ moves every wide diagram in the chat across, each to its own edge; typing, it moves by words | P1 |

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const wideDiagram = "```mermaid\ngraph LR\n  A[Agent loop] --> B[Tool registry] --> C[Permissions policy] --> D[Bash runner] --> E[Job store] --> F[Terminal UI] --> G[Last box here]\n```"

func TestWideDrawings(t *testing.T) {
	t.Run("WD-01 a diagram wider than the conversation says so under it; one that fits says nothing", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(60)

		wide := ansi.Strip(p.Answer(wideDiagram))
		narrow := ansi.Strip(p.Answer("```mermaid\ngraph LR\n  A --> B\n```"))

		if !strings.Contains(wide, "columns out of view · ctrl+←→ moves across · a click shows it whole") {
			t.Errorf("wide:\n%s", wide)
		}
		if strings.Contains(narrow, "out of view") {
			t.Errorf("narrow:\n%s", narrow)
		}
		again := p.Answer(wideDiagram)
		if len(p.drawings) != 2 || again == "" {
			t.Errorf("each diagram, drawn again or not, should be kept once: %d", len(p.drawings))
		}
	})

	t.Run("WD-02 a click on a wide diagram opens it whole, scrolled across with ←→, its colours kept", func(t *testing.T) {
		m := sidebarModel(t, 100)
		updated, _ := m.Update(printMsg(m.printer.Answer(wideDiagram)))
		m = updated.(model)
		at := cellOf(t, m, "Agent loop")

		clicked, _ := m.Update(tea.MouseClickMsg{X: at.x, Y: at.y, Button: tea.MouseLeft})
		released, _ := clicked.(model).Update(tea.MouseReleaseMsg{X: at.x, Y: at.y, Button: tea.MouseLeft})
		m = released.(model)
		if m.viewer == nil || m.viewer.drawing == "" {
			t.Fatal("the click should open the diagram")
		}
		first := ansi.Strip(m.View().Content)
		for range 20 {
			moved, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			m = moved.(model)
		}
		across := ansi.Strip(m.View().Content)

		if !strings.Contains(first, "Agent loop") || !strings.HasPrefix(strings.Split(first, "\n")[1], "┌") || !strings.Contains(first, "Diagram") {
			t.Errorf("opened:\n%s", first)
		}
		if !strings.Contains(across, "Last box here") || strings.Contains(across, "Agent loop") {
			t.Errorf("scrolled across:\n%s", across)
		}
		if m.viewer.colored == nil || !strings.Contains(m.viewer.colored[0], "\x1b[") {
			t.Error("the diagram should keep its colours")
		}
	})

	t.Run("WD-03 dragging pans the diagram, without copying; shift and the wheel move it across", func(t *testing.T) {
		m := sidebarModel(t, 100)
		updated, _ := m.Update(printMsg(m.printer.Answer(wideDiagram)))
		m = updated.(model)
		at := cellOf(t, m, "Agent loop")
		updated, _ = m.Update(tea.MouseClickMsg{X: at.x, Y: at.y, Button: tea.MouseLeft})
		updated, _ = updated.(model).Update(tea.MouseReleaseMsg{X: at.x, Y: at.y, Button: tea.MouseLeft})
		m = updated.(model)

		updated, _ = m.Update(tea.MouseClickMsg{X: 60, Y: 2, Button: tea.MouseLeft})
		updated, _ = updated.(model).Update(tea.MouseMotionMsg{X: 30, Y: 2, Button: tea.MouseLeft})
		updated, cmd := updated.(model).Update(tea.MouseReleaseMsg{X: 30, Y: 2, Button: tea.MouseLeft})
		m = updated.(model)
		dragged := m.viewer.left
		updated, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, Mod: tea.ModShift})
		back := updated.(model).viewer.left

		if dragged != 5 || cmd != nil || m.viewer.notice != "" {
			t.Errorf("dragged to %d (the diagram is 105 wide on 100), cmd %v notice %q", dragged, cmd != nil, m.viewer.notice)
		}
		if back != 0 {
			t.Errorf("shift and the wheel should move it back: %d", back)
		}
		m = updated.(model)
		press := func(text string) {
			moved, _ := m.Update(tea.KeyPressMsg{Code: rune(text[0]), Text: text})
			m = moved.(model)
		}
		press("l")
		right := m.viewer.left
		press("h")
		if right != 5 || m.viewer.left != 0 {
			t.Errorf("l and h should pan across: %d then %d", right, m.viewer.left)
		}
	})

	t.Run("WD-04 ctrl+←→ moves every wide diagram in the chat across, each to its own edge; typing, it moves by words", func(t *testing.T) {
		m := sidebarModel(t, 100)
		wider := strings.Replace(wideDiagram, "G[Last box here]", "G[Last box here] --> H[And one more box] --> I[And the last]", 1)
		for _, diagram := range []string{wider, wideDiagram} {
			updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer(diagram) }))
			m = updated.(model)
		}
		press := func(code rune, times int) {
			for range times {
				updated, _ := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
				m = updated.(model)
			}
		}
		lefts := func() (int, int) { return m.printer.drawings[0].left, m.printer.drawings[1].left }
		edges := []int{m.printer.drawings[0].widest - m.printer.width, m.printer.drawings[1].widest - m.printer.width}

		press(tea.KeyRight, 1)
		oneStep0, oneStep1 := lefts()
		press(tea.KeyRight, 30)
		end0, end1 := lefts()
		press(tea.KeyLeft, 1)
		back0, back1 := lefts()
		shown := ansi.Strip(m.conversationContent())

		if edges[0] <= edges[1] || edges[1] <= 0 {
			t.Fatalf("the diagrams should be of two widths over the room: %v", edges)
		}
		if oneStep0 != min(drawingStep, edges[0]) || oneStep1 != min(drawingStep, edges[1]) {
			t.Errorf("one step: %d %d", oneStep0, oneStep1)
		}
		if end0 != edges[0] || end1 != edges[1] {
			t.Errorf("each should stop at its own edge: %d %d, edges %v", end0, end1, edges)
		}
		if back0 != edges[0]-drawingStep || back1 != max(edges[1]-drawingStep, 0) {
			t.Errorf("back, each from its own place: %d %d", back0, back1)
		}
		if want := fmt.Sprintf("◂ %d · %d ▸ columns out of view", back0, edges[0]-back0); !strings.Contains(shown, want) {
			t.Errorf("the line under should say %q:\n%s", want, shown)
		}

		m.input.SetValue("some words here")
		before0, _ := lefts()
		press(tea.KeyLeft, 1)
		after0, _ := lefts()
		if before0 != after0 {
			t.Error("while typing, ctrl+← should move by words, not the diagrams")
		}
	})

	t.Run("WD-05 a right click on a diagram offers its code: shown in its place and back, copied; s does so whole", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", "")
		before := flashTime
		flashTime = time.Millisecond
		t.Cleanup(func() { flashTime = before })
		m := sidebarModel(t, 100)
		small := "```mermaid\ngraph LR\n  Start --> Finish\n```"
		updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer(small) }))
		m = updated.(model)
		at := cellOf(t, m, "Start")
		choose := func(option string) tea.Cmd {
			updated, _ := m.Update(tea.MouseClickMsg{X: at.x, Y: at.y, Button: tea.MouseRight})
			m = updated.(model)
			if m.popup == nil {
				t.Fatal("a right click on a diagram should open its menu")
			}
			for i, each := range m.popup.options {
				if each.Value == option {
					chosen, cmd := m.choosePopup(i)
					m = chosen.(model)
					return cmd
				}
			}
			t.Fatalf("no option %q", option)
			return nil
		}

		choose("code")
		code := ansi.Strip(m.conversationContent())
		at = cellOf(t, m, "Start --> Finish")
		choose("code")
		back := ansi.Strip(m.conversationContent())
		at = cellOf(t, m, "Start")
		copied := sent(choose("copy"))

		if !strings.Contains(code, "Start --> Finish") || strings.Contains(code, "┌") {
			t.Errorf("its code should show in its place:\n%s", code)
		}
		if strings.Contains(back, "Start --> Finish") || !strings.Contains(back, "┌") {
			t.Errorf("the drawing should come back:\n%s", back)
		}
		if !strings.Contains(copied, "graph LR\n  Start --> Finish") {
			t.Errorf("copied = %q", copied)
		}

		opened, _ := m.openDrawing(m.printer.drawings[0])
		m = opened.(model)
		drawnFirst := strings.Join(m.viewer.lines, "\n")
		pressed, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		m = pressed.(model)
		if !strings.Contains(drawnFirst, "┌") || m.viewer.lines[1] != "  Start --> Finish" || !strings.Contains(ansi.Strip(m.View().Content), "Diagram code") {
			t.Errorf("s should show the code whole: %q", m.viewer.lines)
		}
	})

	t.Run("WD-06 a menu option clicked opens nothing under it; ctrl+↑↓ and, with nothing typed, ctrl+hjkl scroll and pan; /diagrams switches all", func(t *testing.T) {
		m := sidebarModel(t, 100)
		for _, diagram := range []string{wideDiagram, "```mermaid\ngraph LR\n  Start --> Finish\n```"} {
			updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer(diagram) }))
			m = updated.(model)
		}
		for i := range 40 {
			updated, _ := m.Update(printMsg(m.printer.Note(fmt.Sprintf("line %d", i))))
			m = updated.(model)
		}
		m.scroll(true, 200)
		at := cellOf(t, m, "Agent loop")
		updated, _ := m.Update(tea.MouseClickMsg{X: at.x, Y: at.y, Button: tea.MouseRight})
		m = updated.(model)
		box := m.popupLayout()
		row := -1
		for r, i := range box.options {
			if m.popup.options[i].Value == "all" {
				row = r
			}
		}
		updated, _ = m.Update(tea.MouseClickMsg{X: box.x + 2, Y: box.y + row, Button: tea.MouseLeft})
		updated, _ = updated.(model).Update(tea.MouseReleaseMsg{X: at.x, Y: at.y, Button: tea.MouseLeft})
		m = updated.(model)
		allCode := m.printer.drawings[0].code && m.printer.drawings[1].code
		if m.viewer != nil || !allCode {
			t.Fatalf("the option should switch all to code and open nothing: viewer %v all code %v", m.viewer != nil, allCode)
		}
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
		m = updated.(model)
		m.input.SetValue("/diagrams")
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(model)
		if m.printer.drawings[0].code || m.printer.drawings[1].code {
			t.Errorf("/diagrams should draw them all again")
		}

		m.input.SetValue("")
		key := func(code rune) {
			updated, _ := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
			m = updated.(model)
		}
		m.scrolled = false
		offset := m.conversationOffset()
		key(tea.KeyUp)
		up := m.conversationOffset()
		key('k')
		upAgain := m.conversationOffset()
		key('l')
		panned := m.printer.drawings[0].left
		m.input.SetValue("typing")
		key('j')

		if up != offset-1 || upAgain != offset-2 {
			t.Errorf("ctrl+↑ and ctrl+k should scroll: %d %d %d", offset, up, upAgain)
		}
		if edge := m.printer.drawings[0].widest - m.printer.width; panned != min(drawingStep, edge) {
			t.Errorf("ctrl+l should pan, to the edge at most: %d, edge %d", panned, edge)
		}
		if m.input.Value() != "typing\n" {
			t.Errorf("while typing ctrl+j is a new line: %q", m.input.Value())
		}
	})

	t.Run("WD-07 a right click on an SVG shows its code in its place and the picture back, or copies it", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", "")
		before := flashTime
		flashTime = time.Millisecond
		t.Cleanup(func() { flashTime = before })
		m := sidebarModel(t, 100)
		svg := "Here:\n\n```svg\n<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"10\" height=\"10\"><rect width=\"10\" height=\"10\"/></svg>\n```\n\nDone."
		updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer(svg) }))
		m = updated.(model)
		pick := func(text, option string) tea.Cmd {
			at := cellOf(t, m, text)
			updated, _ := m.Update(tea.MouseClickMsg{X: at.x, Y: at.y, Button: tea.MouseRight})
			m = updated.(model)
			if m.popup == nil || m.popup.title != "Picture" {
				t.Fatalf("a right click on the picture should open its menu: %+v", m.popup)
			}
			for i, each := range m.popup.options {
				if each.Value == option {
					chosen, cmd := m.choosePopup(i)
					m = chosen.(model)
					return cmd
				}
			}
			t.Fatalf("no option %q", option)
			return nil
		}

		pick("[svg image]", "code")
		code := ansi.Strip(m.conversationContent())
		pick("<rect", "code")
		back := ansi.Strip(m.conversationContent())
		copied := sent(pick("[svg image]", "copy"))

		if !strings.Contains(code, `<rect width="10"`) || strings.Contains(code, "[svg image]") || !strings.Contains(code, "Done.") {
			t.Errorf("its code should show in its place:\n%s", code)
		}
		if !strings.Contains(back, "[svg image]") || strings.Contains(back, "<rect") {
			t.Errorf("the picture should come back:\n%s", back)
		}
		if !strings.Contains(copied, "<svg xmlns") {
			t.Errorf("copied = %q", copied)
		}
	})

	t.Run("WD-08 with kitty images, an SVG written or a .svg file shown swaps its picture for its code and back", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="red"/></svg>`
		for name, answer := range map[string]string{
			"written": "Here:\n\n```svg\n" + svg + "\n```\n\nDone.",
			"a file":  "Here:\n\n![logo](logo.svg)\n\nDone.",
		} {
			m := sidebarModel(t, 100)
			m.printer.workDir = t.TempDir()
			if err := os.WriteFile(filepath.Join(m.printer.workDir, "logo.svg"), []byte(svg), 0o644); err != nil {
				t.Fatal(err)
			}
			m.printer.setImages(render.ModeKitty)
			updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer(answer) }))
			m = updated.(model)
			swap := func() {
				y := -1
				for i, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
					if strings.ContainsRune(line, '\U0010eeee') || strings.Contains(line, "<rect") {
						y = i
						break
					}
				}
				updated, _ := m.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseRight})
				m = updated.(model)
				if m.popup == nil || m.popup.title != "Picture" {
					t.Fatalf("%s: no picture menu at row %d", name, y)
				}
				chosen, _ := m.choosePopup(0)
				m = chosen.(model)
			}

			swap()
			code := ansi.Strip(m.conversationContent())
			swap()
			back := ansi.Strip(m.conversationContent())

			if !strings.Contains(code, "<rect") || strings.ContainsRune(code, '\U0010eeee') {
				t.Errorf("%s: its code should replace the picture:\n%s", name, code)
			}
			if strings.Contains(back, "<rect") || !strings.ContainsRune(back, '\U0010eeee') {
				t.Errorf("%s: the picture should come back", name)
			}
		}
	})
}
