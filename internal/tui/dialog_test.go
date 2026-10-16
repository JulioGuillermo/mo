package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | DG-01 | Happy    | Tabs show how many parts there are and which is shown; all answered sends them in order | P0 |
// | DG-02 | Happy    | A question takes an option, several ticked, or text typed; empty text skips | P0 |
// | DG-03 | Happy    | "Always this session" also answers the other calls to that tool waiting | P1     |
// | DG-04 | Happy    | Rules added from one call let through the others they cover          | P1       |
// | DG-05 | Happy    | The dialog stays above the input, the conversation scrolled or not   | P1       |
// | DG-06 | Happy    | A long command scrolls to its end; a long line wraps instead of being cut | P1   |
// | DG-07 | Happy    | Each part of a bash command shows what the rules say of it            | P1       |
// | DG-09 | Happy    | A command of many parts scrolls with them, every part reachable       | P1       |
// | DG-08 | Happy    | What a later call asks joins the open dialog; each ask is answered once its parts are | P0 |
// | HL-01 | Happy    | Bash is coloured by what each piece is, on the block's background     | P2       |
// | FC-01 | Happy    | An opened edit shows its diff highlighted; an opened write what it wrote | P1    |

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

func asking(interaction agent.Interaction) (model, chan agent.Reply) {
	m := conversationModel()
	m.running, m.cancel = true, func() {}
	reply := make(chan agent.Reply, 1)
	updated, _ := m.Update(interactionMsg{interaction: interaction, reply: reply})
	return updated.(model), reply
}

func keys(t *testing.T, m model, pressed ...tea.KeyPressMsg) model {
	t.Helper()
	for _, key := range pressed {
		updated, _ := m.Update(key)
		m = updated.(model)
	}
	return m
}

var (
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyN     = tea.KeyPressMsg{Code: 'n', Text: "n"}
	keyA     = tea.KeyPressMsg{Code: 'a', Text: "a"}
)

func bash(command string) agent.ApprovalRequest {
	return agent.ApprovalRequest{Call: llm.ToolCall{Name: "bash"}, Summary: command}
}

func TestDialog(t *testing.T) {
	t.Run("DG-01 tabs show how many parts there are and which is shown; all answered sends them in order", func(t *testing.T) {
		m, reply := asking(agent.Interaction{
			Approvals: []agent.ApprovalRequest{bash("make"), {Call: llm.ToolCall{Name: "fetch"}, Summary: "https://go.dev"}},
			Questions: []tools.Question{{Text: "Which branch?"}},
		})
		first := ansi.Strip(m.dialogView())

		m = keys(t, m, keyTab, keyY)
		second := ansi.Strip(m.dialogView())
		for _, r := range "main" {
			m = keys(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		m = keys(t, m, keyEnter, keyN)

		if !strings.Contains(first, "⚑ bash") || !strings.Contains(first, "? Which branch?") || !strings.Contains(first, "1 of 3 · 3 left") || !strings.Contains(first, "Run bash?") {
			t.Errorf("first:\n%s", first)
		}
		if !strings.Contains(second, "✓ fetch") || !strings.Contains(second, "3 of 3 · 2 left") || !strings.Contains(second, "Which branch?") {
			t.Errorf("second:\n%s", second)
		}
		got := receive(t, reply)
		if m.dialog != nil || got.Decisions[0] != agent.Deny || got.Decisions[1] != agent.Allow || got.Answers[0] != "main" {
			t.Errorf("reply = %+v", got)
		}
	})

	t.Run("DG-02 a question takes an option, several ticked, or text typed; empty text skips", func(t *testing.T) {
		m, reply := asking(agent.Interaction{Questions: []tools.Question{
			{Text: "Db?", Options: []string{"Postgres", "SQLite"}},
			{Text: "Features?", Options: []string{"auth", "api", "ui"}, Multiple: true},
			{Text: "Anything else?", Options: []string{"no"}},
		}})

		m = keys(t, m, keyDown, keyEnter)
		m = keys(t, m, keySpace, keyDown, keyDown, keySpace, keyEnter)
		m = keys(t, m, keyDown, keyEnter)

		got := receive(t, reply)
		if got.Answers[0] != "SQLite" || got.Answers[1] != "auth, ui" || got.Answers[2] != "" || m.dialog != nil {
			t.Errorf("answers = %q", got.Answers)
		}
	})

	t.Run("DG-03 always this session also answers the other calls to that tool waiting", func(t *testing.T) {
		m, reply := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{
			{Call: llm.ToolCall{Name: "rm"}, Summary: "a"}, bash("ls"), {Call: llm.ToolCall{Name: "rm"}, Summary: "b"},
		}})

		m = keys(t, m, keyA)
		left := m.dialog.current
		m = keys(t, m, keyY)

		got := receive(t, reply)
		if left != 1 || got.Decisions[0] != agent.AllowAlways || got.Decisions[1] != agent.Allow || got.Decisions[2] != agent.Allow {
			t.Errorf("left = %d decisions = %v", left, got.Decisions)
		}
	})

	t.Run("DG-04 rules added from one call let through the others they cover", func(t *testing.T) {
		m, reply := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{
			{Call: llm.ToolCall{Name: "bash"}, Summary: "make test", Ruled: true, Commands: []string{"make test"}},
			{Call: llm.ToolCall{Name: "bash"}, Summary: "make lint", Ruled: true, Commands: []string{"make lint"}},
			{Call: llm.ToolCall{Name: "bash"}, Summary: "rm -rf x", Ruled: true, Commands: []string{"rm -rf x"}},
		}})
		policy, _ := permissions.New(permissions.ModeEdits, []string{"make *"}, nil)

		m.allowByRules(policy)
		moved, _ := m.next()
		m = keys(t, moved.(model), keyN)

		got := receive(t, reply)
		if got.Decisions[0] != agent.Allow || got.Decisions[1] != agent.Allow || got.Decisions[2] != agent.Deny {
			t.Errorf("decisions = %v", got.Decisions)
		}
	})

	t.Run("DG-05 the dialog stays above the input, the conversation scrolled or not", func(t *testing.T) {
		m, _ := asking(agent.Interaction{Questions: []tools.Question{{Text: "Which branch?"}}})
		for range 50 {
			updated, _ := m.Update(printMsg("line"))
			m = updated.(model)
		}
		m.scroll(true, 30)

		if !strings.Contains(ansi.Strip(m.View().Content), "Which branch?") {
			t.Error("the question went out of sight")
		}
	})
}

// receive waits for the dialog's reply, failing instead of hanging.
func receive(t *testing.T, reply chan agent.Reply) agent.Reply {
	t.Helper()
	select {
	case got := <-reply:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("the dialog sent no reply")
		return agent.Reply{}
	}
}

func TestCommandView(t *testing.T) {
	long := strings.Repeat("echo line\n", 40) + "rm -rf build"

	t.Run("DG-06 a long command scrolls to its end; a long line wraps instead of being cut", func(t *testing.T) {
		m, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{bash(long), bash("echo " + strings.Repeat("x", 300))}})
		m.width, m.height = 80, 30
		top := ansi.Strip(m.dialogView())

		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnd})
		bottom := ansi.Strip(m.dialogView())
		m = keys(t, m, keyTab)
		wrapped := ansi.Strip(m.dialogView())

		if strings.Contains(top, "rm -rf build") || !strings.Contains(top, "lines 1–") || !strings.Contains(bottom, "rm -rf build") {
			t.Errorf("top:\n%s\nbottom:\n%s", top, bottom)
		}
		if strings.Contains(wrapped, "…") || strings.Count(wrapped, "xxxxxxxxxx") < 4 {
			t.Errorf("wrapped:\n%s", wrapped)
		}
	})

	t.Run("DG-07 each part of a bash command shows what the rules say of it", func(t *testing.T) {
		m, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{bash(`ls -la && git push origin main; echo "hello  world" > out.txt`)}})
		m.session.Agent.Permissions, _ = permissions.New(permissions.ModeEdits, []string{"ls *", "echo *"}, []string{"git push *"})

		view := ansi.Strip(m.dialogView())

		for _, want := range []string{"3 parts", "✓ ls -la", "allowed: ls *", "✗ git push origin main", "deny rule: git push *",
			`✎ echo "hello  world" > out.txt`, "allowed: echo * · writes a file"} {
			if !strings.Contains(view, want) {
				t.Errorf("missing %q in:\n%s", want, view)
			}
		}
	})

	t.Run("DG-08 what a later call asks joins the open dialog; each ask is answered once its parts are", func(t *testing.T) {
		m, first := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{bash("rm -rf build")}})
		second := make(chan agent.Reply, 1)
		updated, _ := m.Update(interactionMsg{interaction: agent.Interaction{Approvals: []agent.ApprovalRequest{bash("make test")}}, reply: second})
		m = updated.(model)
		tabs := ansi.Strip(m.View().Content)

		m = keys(t, m, keyN)
		var gotFirst agent.Reply
		select {
		case gotFirst = <-first:
		default:
			t.Fatal("the first ask should be answered as soon as its part is")
		}
		stillOpen := m.dialog != nil
		select {
		case <-second:
			t.Fatal("the second ask is not answered yet")
		default:
		}
		m = keys(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
		gotSecond := <-second

		if m.dialog != nil {
			t.Error("the dialog should close once everything is answered")
		}
		if !strings.Contains(tabs, "2") || !stillOpen || gotFirst.Decisions[0] != agent.Deny || gotSecond.Decisions[0] != agent.Allow {
			t.Errorf("open %v first %+v second %+v tabs:\n%s", stillOpen, gotFirst, gotSecond, tabs)
		}
	})

	t.Run("DG-09 a command of many parts scrolls with them, every part reachable", func(t *testing.T) {
		var parts []string
		for i := 1; i <= 20; i++ {
			parts = append(parts, fmt.Sprintf("echo part%02d", i))
		}
		m, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{{Call: llm.ToolCall{Name: "bash"}, Summary: strings.Join(parts, "; "), Ruled: true}}})
		m.width, m.height = 80, 30
		few, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{{Call: llm.ToolCall{Name: "bash"}, Summary: "cd src && ls -la | grep x && git push origin main; echo done > out.txt", Ruled: true}}})
		few.width, few.height = 80, 30
		if shown := ansi.Strip(few.dialogView()); !strings.Contains(shown, "✎ echo done > out.txt") || strings.Contains(shown, "lines 1–") {
			t.Errorf("a command of a few parts should show them all, unscrolled:\n%s", shown)
		}
		top := ansi.Strip(m.dialogView())
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEnd})
		bottom := ansi.Strip(m.dialogView())

		if !strings.Contains(top, "lines 1–") || strings.Contains(top, "● echo part20") || strings.Contains(top, "more") {
			t.Errorf("top:\n%s", top)
		}
		if !strings.Contains(bottom, "● echo part20") {
			t.Errorf("the last part should be reached:\n%s", bottom)
		}
	})

	t.Run("HL-01 bash is coloured by what each piece is, on the block's background", func(t *testing.T) {
		lines := highlightLines("for f in *.go; do echo 'x' \"$f\"; done # list", "bash", 80, lipgloss.Color("#0e1630"), true)
		plain := highlightLines("for f in x", "", 80, lipgloss.Color("#0e1630"), true)

		if len(lines) != 1 || !strings.Contains(lines[0], "187;154;247") || !strings.Contains(lines[0], "158;206;106") || !strings.Contains(lines[0], "224;175;104") || !strings.Contains(lines[0], "48;2;14;22;48") {
			t.Errorf("line = %q", lines[0])
		}
		if strings.Contains(plain[0], "187;154;247") {
			t.Errorf("plain text was coloured: %q", plain[0])
		}
	})
}

func TestFileChanges(t *testing.T) {
	t.Run("FC-01 an opened edit shows its diff highlighted; an opened write shows what it wrote", func(t *testing.T) {
		m := conversationModel()
		editArgs := []byte(`{"path":"a.go","old":"x := 1\n","new":"x := 2\ny := 3\n"}`)
		writeArgs := []byte(`{"path":"b.py","content":"def f():\n    return 1\n"}`)
		for _, call := range []llm.ToolCall{{ID: "e", Name: "edit", Args: editArgs}, {ID: "w", Name: "write", Args: writeArgs}} {
			updated, _ := m.Update(toolStartMsg{call: call, summary: "file"})
			updated, _ = updated.(model).Update(toolDoneMsg{id: call.ID, output: "done"})
			m = updated.(model)
		}
		folded := shown(m)
		opened, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		view := shown(opened.(model))

		if !strings.Contains(folded, "+2 −1") || !strings.Contains(folded, "2 lines") || strings.Contains(folded, "y := 3") {
			t.Errorf("folded:\n%s", folded)
		}
		for _, want := range []string{"- x := 1", "+ x := 2", "+ y := 3", "def f():", "return 1"} {
			if !strings.Contains(view, want) {
				t.Errorf("missing %q in:\n%s", want, view)
			}
		}
		if strings.Contains(view, "▌ \n") {
			t.Errorf("an empty line slipped into the change:\n%s", view)
		}
	})
}
