package tui

// Test Traceability Table
// | ID    | Category | Description                                                               | Priority |
// | UD-01 | Happy    | The model's interface is listed in the panel, new until opened as a dialog | P0      |
// | UD-02 | Happy    | Typing fills the focused input; submit sends every value to the model    | P0       |
// | UD-03 | Happy    | A click outside or close hides the dialog; remove takes it away and tells the model | P1 |
// | UD-04 | Happy    | A script reacts to typing in the dialog; its error shows under the interface | P0   |
// | UD-06 | Happy    | A grid lines each column's cells up, as wide as the widest, a fixed width kept, a click reaching a cell | P1 |
// | UD-07 | Happy    | An opened ui call shows its tree under guides, as mended, and its script highlighted; folded, a hint says so | P1 |
// | UD-08 | Happy    | A click on a select's value lists its options over the interface; one taken is set; a long list scrolls | P1 |
// | UD-05 | Happy    | Select, slider, checkbox, switch and radio are drawn, change by keys and clicks, and go with submit | P0 |

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func uiModel(t *testing.T) (model, *tools.UIs) {
	t.Helper()
	m := sidebarModel(t, 140)
	uis := tools.NewUIs("")
	uis.SetConversation("c1")
	m.session.Agent = &agent.Agent{Tools: tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Jobs: m.jobs(), UIs: uis}, tools.Options{})}
	m.running = true
	tree := `{"type":"card","title":"Sign up","children":[
	  {"type":"input","id":"email","placeholder":"you@example.com"},
	  {"type":"panel","direction":"row","gap":2,"children":[
	    {"type":"button","id":"cancel","label":"Cancel","on_click":"close"},
	    {"type":"button","id":"drop","label":"Drop","on_click":"remove"},
	    {"type":"button","id":"ok","label":"Create","on_click":"submit"}]}]}`
	output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: []byte(`{"action":"create","title":"Sign up","tree":` + tree + `}`)})
	if isError {
		t.Fatalf("create: %s", output)
	}
	return m, uis
}

func TestUIDialog(t *testing.T) {
	t.Run("UD-01 the model's interface is listed in the panel, new until opened as a dialog", func(t *testing.T) {
		m, uis := uiModel(t)
		panel := ansi.Strip(m.View().Content)

		m = clickOn(t, m, "Sign up")
		screen := ansi.Strip(m.View().Content)
		ui, _ := uis.Get(1)

		if !strings.Contains(panel, "UIs") || !strings.Contains(panel, "● 1 Sign up") {
			t.Errorf("panel:\n%s", panel)
		}
		if m.uiOpen != 1 || !ui.Seen || !strings.Contains(screen, "UI 1 · Sign up") || !strings.Contains(screen, "Create") || !strings.Contains(screen, "Jobs") {
			t.Errorf("dialog:\n%s", screen)
		}
	})

	t.Run("UD-02 typing fills the focused input; submit sends every value to the model", func(t *testing.T) {
		m, uis := uiModel(t)
		updated, _ := m.openUI(1)
		m = updated.(model)
		for _, key := range []tea.KeyPressMsg{{Code: 'a', Text: "a"}, {Code: '@', Text: "@"}, {Code: 'b', Text: "b"}, {Code: tea.KeyTab}, {Code: tea.KeyTab}, {Code: tea.KeyTab}} {
			updated, _ = m.Update(key)
			m = updated.(model)
		}
		typed := ansi.Strip(m.View().Content)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(model)
		events := uis.TakeEvents()

		if !strings.Contains(typed, "a@b") || m.uiOpen != 0 || len(events) != 1 {
			t.Fatalf("open = %d events = %+v screen:\n%s", m.uiOpen, events, typed)
		}
		if note := events[0].Note(); note != `UI 1 "Sign up": submitted with ok; values: email="a@b"` {
			t.Errorf("note = %q", note)
		}
	})

	t.Run("UD-03 a click outside or close hides the dialog; remove takes it away and tells the model", func(t *testing.T) {
		m, uis := uiModel(t)
		updated, _ := m.openUI(1)
		hidden, _ := updated.(model).Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}))
		stillListed := len(uis.List())
		reopened, _ := hidden.(model).openUI(1)
		m = reopened.(model)
		frame, _ := m.uiFrame()
		hits := map[string]uiHit{}
		for _, hit := range frame.hits {
			hits[hit.id] = hit
		}
		closed, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: hits["cancel"].x0, Y: hits["cancel"].y0, Button: tea.MouseLeft}))
		afterClose, told := len(uis.List()), uis.HasEvents()
		reopened, _ = closed.(model).openUI(1)
		removed, _ := reopened.(model).Update(tea.MouseClickMsg(tea.Mouse{X: hits["drop"].x0, Y: hits["drop"].y0, Button: tea.MouseLeft}))
		events := uis.TakeEvents()

		if hidden.(model).uiOpen != 0 || stillListed != 1 {
			t.Error("a click outside should only hide it")
		}
		if closed.(model).uiOpen != 0 || afterClose != 1 || told {
			t.Errorf("close should only hide it: open = %d listed = %d told = %v", closed.(model).uiOpen, afterClose, told)
		}
		if removed.(model).uiOpen != 0 || len(uis.List()) != 0 || len(events) != 1 || events[0].Action != tools.UIRemove {
			t.Errorf("removed: open = %d left = %d events = %+v", removed.(model).uiOpen, len(uis.List()), events)
		}
	})

	t.Run("UD-04 a script reacts to typing in the dialog; its error shows under the interface", func(t *testing.T) {
		m, uis := uiModel(t)
		tree := `{"type":"panel","children":[
		  {"type":"input","id":"name","on_input":"check"},
		  {"type":"label","id":"hint","text":"too short","visible":false},
		  {"type":"button","id":"boom","label":"Boom","on_click":"boom"}]}`
		script := "function check(e) ui.set('hint', {visible = #e.value < 3}) end\nfunction boom() error('it broke') end"
		args, _ := json.Marshal(map[string]any{"action": "create", "title": "Name", "tree": json.RawMessage(tree), "script": script})
		if output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args}); isError {
			t.Fatal(output)
		}
		updated, _ := m.openUI(2)
		m = updated.(model)
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		m = updated.(model)
		short := ansi.Strip(m.View().Content)
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		updated, _ = updated.(model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(model)
		broken := ansi.Strip(m.View().Content)

		if !strings.Contains(short, "too short") {
			t.Errorf("the hint should show:\n%s", short)
		}
		if !strings.Contains(broken, "script:") || !strings.Contains(broken, "it broke") || !uis.HasEvents() {
			t.Errorf("the error should show and be told:\n%s", broken)
		}
	})

	t.Run("UD-05 select, slider, checkbox, switch and radio are drawn, change by keys and clicks, and go with submit", func(t *testing.T) {
		m, uis := uiModel(t)
		tree := `{"type":"panel","children":[
		  {"type":"select","id":"size","label":"Size","options":["S","M","L"],"value":"M","on_change":"notify"},
		  {"type":"slider","id":"vol","label":"Vol","min":0,"max":10,"value":"4"},
		  {"type":"checkbox","id":"terms","label":"I agree"},
		  {"type":"switch","id":"dark","label":"Dark"},
		  {"type":"radio","id":"plan","options":["free","pro"],"direction":"row"},
		  {"type":"button","id":"go","label":"Save","on_click":"submit"}]}`
		args, _ := json.Marshal(map[string]any{"action": "create", "title": "Prefs", "tree": json.RawMessage(tree)})
		if output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args}); isError {
			t.Fatal(output)
		}
		updated, _ := m.openUI(2)
		m = updated.(model)
		drawn := ansi.Strip(m.View().Content)
		press := func(keys ...tea.KeyPressMsg) {
			for _, key := range keys {
				updated, _ := m.Update(key)
				m = updated.(model)
			}
		}
		right, tab, space := tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		press(right)
		told := uis.TakeEvents()
		press(tab, right, right, tab, space, tab, space)
		frame, _ := m.uiFrame()
		var pro, track uiHit
		for _, hit := range frame.hits {
			switch {
			case hit.id == "plan" && hit.part == 2:
				pro = hit
			case hit.id == "vol":
				track = hit
			}
		}
		updated, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: pro.x0, Y: pro.y0, Button: tea.MouseLeft}))
		updated, _ = updated.(model).Update(tea.MouseClickMsg(tea.Mouse{X: track.x1 - 1, Y: track.y0, Button: tea.MouseLeft}))
		m = updated.(model)
		ui, _ := uis.Get(2)
		values := map[string]string{}
		for _, id := range []string{"size", "vol", "terms", "dark", "plan"} {
			node, _ := findNode(ui.Shown(), id)
			values[id] = node.Current()
		}

		for _, want := range []string{"Size ◂ M ▸", "Vol ━━━━━━━━", "[ ] I agree", " ○  Dark", "( ) free  ( ) pro"} {
			if !strings.Contains(drawn, want) {
				t.Errorf("%q not drawn:\n%s", want, drawn)
			}
		}
		if len(told) != 1 || told[0].Note() != `UI 2 "Prefs": change on size, value "L"` {
			t.Errorf("the select's on_change should tell: %+v", told)
		}
		want := map[string]string{"size": "L", "vol": "10", "terms": "true", "dark": "true", "plan": "pro"}
		for id, value := range want {
			if values[id] != value {
				t.Errorf("%s = %q, want %q (all %v)", id, values[id], value, values)
			}
		}
	})

	t.Run("UD-06 a grid lines each column's cells up, as wide as the widest, a fixed width kept, a click reaching a cell", func(t *testing.T) {
		m, uis := uiModel(t)
		tree := `{"type":"panel","children":[
		  {"type":"grid","columns":2,"gap":2,"children":[
		    {"type":"label","text":"Name"},{"type":"input","id":"name"},
		    {"type":"label","text":"Volume level"},{"type":"checkbox","id":"tick","label":"on"}]},
		  {"type":"grid","widths":[10,0],"children":[{"type":"label","text":"A"},{"type":"label","text":"x"}]}]}`
		args, _ := json.Marshal(map[string]any{"action": "create", "title": "Grid", "tree": json.RawMessage(tree)})
		if output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args}); isError {
			t.Fatal(output)
		}
		updated, _ := m.openUI(2)
		m = updated.(model)
		var rows []string
		for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			for _, word := range []string{"Name", "Volume level", "A "} {
				if at := strings.Index(line, word); at >= 0 {
					cell, _, _ := strings.Cut(line[at:], "│")
					rows = append(rows, strings.TrimRight(cell, " "))
				}
			}
		}
		frame, _ := m.uiFrame()
		var tick uiHit
		for _, hit := range frame.hits {
			if hit.id == "tick" {
				tick = hit
			}
		}
		updated, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: tick.x0, Y: tick.y0, Button: tea.MouseLeft}))
		ui, _ := uis.Get(2)
		ticked, _ := findNode(ui.Shown(), "tick")

		if len(rows) != 3 || !strings.HasPrefix(rows[0], "Name          ▏") || rows[1] != "Volume level  [ ] on" || !strings.HasPrefix(rows[2], "A         x") {
			t.Errorf("rows = %q", rows)
		}
		if !ticked.Checked() {
			t.Error("a click on the cell should tick it")
		}
	})
}

func TestUICallOpened(t *testing.T) {
	t.Run("UD-07 an opened ui call shows its tree as an outline, as mended, and its script; folded, a hint says so", func(t *testing.T) {
		m := sidebarModel(t, 100)
		args, _ := json.Marshal(map[string]any{"action": "create", "title": "Vol", "script": "function moved(e)\n  ui.set('show', {text = e.value})\nend",
			"tree": map[string]any{"type": "card", "title": "Vol", "children": []any{
				map[string]any{"type": "slider", "id": "vol", "on_change": "moved", "max": 10},
				map[string]any{"type": "btn", "id": "ok", "text": "OK", "onClick": "submit"}}}})
		run := toolRun{name: "ui", summary: "create Vol", args: args, done: true, output: "UI 1 created"}
		folded := ansi.Strip(m.printer.Tool(run, false))
		opened := ansi.Strip(m.printer.Tool(run, true))

		if strings.Contains(folded, "card") || !strings.Contains(folded, "ctrl+o or click for the whole call") {
			t.Errorf("folded:\n%s", folded)
		}
		for _, want := range []string{"tree", `card "Vol"`, `├─ slider #vol max=10 on_change="moved"`, `└─ button #ok "OK" on_click="submit"`, "script", "function moved(e)", "  ui.set('show'"} {
			if !strings.Contains(opened, want) {
				t.Errorf("%q not in:\n%s", want, opened)
			}
		}
	})
}

func TestSelectList(t *testing.T) {
	t.Run("UD-08 a click on a select's value lists its options over the interface; one taken is set; a long list scrolls", func(t *testing.T) {
		m, uis := uiModel(t)
		var many []string
		for i := 1; i <= 40; i++ {
			many = append(many, fmt.Sprintf("%q", fmt.Sprintf("option %02d", i)))
		}
		tree := `{"type":"panel","children":[
		  {"type":"select","id":"size","label":"Size","options":["S","M","L"],"value":"M"},
		  {"type":"select","id":"many","options":[` + strings.Join(many, ",") + `]}]}`
		args, _ := json.Marshal(map[string]any{"action": "create", "title": "Pick", "tree": json.RawMessage(tree)})
		if output, isError := m.session.Agent.Tools.Run(context.Background(), llm.ToolCall{Name: "ui", Args: args}); isError {
			t.Fatal(output)
		}
		updated, _ := m.openUI(2)
		m = updated.(model)
		hit := func(id string, part int) uiHit {
			frame, _ := m.uiFrame()
			for _, each := range frame.hits {
				if each.id == id && each.part == part {
					return each
				}
			}
			t.Fatalf("no hit %s/%d", id, part)
			return uiHit{}
		}
		middle := hit("size", uiSelectList)
		listed, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: middle.x0, Y: middle.y0, Button: tea.MouseLeft}))
		m = listed.(model)
		if m.popup == nil || m.popup.selected != 1 || m.uiOpen != 2 {
			t.Fatalf("popup = %+v open = %d", m.popup, m.uiOpen)
		}
		box := m.popupLayout()
		row := -1
		for r, i := range box.options {
			if i == 2 {
				row = r
			}
		}
		chosen, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: box.x + 2, Y: box.y + row, Button: tea.MouseLeft}))
		m = chosen.(model)
		ui, _ := uis.Get(2)
		size, _ := findNode(ui.Shown(), "size")
		if size.Current() != "L" || m.popup != nil || m.uiOpen != 2 {
			t.Errorf("size = %q popup = %v open = %d", size.Current(), m.popup != nil, m.uiOpen)
		}

		m.live(2).focus = "many"
		opened, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = opened.(model)
		first := ansi.Strip(strings.Join(m.popupLayout().lines, "\n"))
		for range 30 {
			down, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m = down.(model)
		}
		later := ansi.Strip(strings.Join(m.popupLayout().lines, "\n"))
		if m.popup == nil || len(m.popupLayout().lines) > m.height || !strings.Contains(first, "↓") || strings.Contains(first, "↑") {
			t.Fatalf("the long list should fit and say there is more:\n%s", first)
		}
		if !strings.Contains(later, "option 31") || !strings.Contains(later, "↑") || strings.Contains(later, "option 01") {
			t.Errorf("the list should scroll with the selection:\n%s", later)
		}
	})
}
