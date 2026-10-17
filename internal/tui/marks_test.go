package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | MK-01 | Happy    | A call's sign says how it stands: an arc turning while it runs, filled when done, a cross when it failed, a bar when stopped | P0 |
// | MK-02 | Happy    | A call being written breathes; reasoning twinkles                        | P1       |
// | MK-03 | Happy    | A call written and not run is dotted while another runs, beating while the user is asked | P1 |
// | MK-04 | Edge     | The arc's place in a block never reaches the screen or what is copied       | P0       |

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

func signOf(t *testing.T, drawn string) string {
	t.Helper()
	line := strings.TrimSpace(strings.Split(strings.TrimLeft(ansi.Strip(drawn), "\n"), "\n")[0])
	sign, _, _ := strings.Cut(line, " ")
	return sign
}

func TestMarks(t *testing.T) {
	p := newPrinter()

	t.Run("MK-01 a call's sign says how it stands", func(t *testing.T) {
		cases := map[string]toolRun{
			markRunning: {name: "bash", summary: "sleep 9"},
			markDone:    {name: "bash", summary: "ls", done: true, output: "a.go"},
			markFailed:  {name: "bash", summary: "false", done: true, isError: true, output: "exit 1"},
			markStopped: {name: "bash", summary: "rm x", done: true, isError: true, output: "The user denied this call. Ask them how to proceed."},
		}
		for want, run := range cases {
			if got := signOf(t, p.Tool(run, false)); got != want {
				t.Errorf("%+v: %q, want %q", run, got, want)
			}
		}
		if !agent.Stopped("Cancelled by the user before it ran.") || agent.Stopped("exit 1") {
			t.Error("stopped results are told wrong")
		}
	})

	t.Run("MK-02 a call being written breathes; reasoning twinkles", func(t *testing.T) {
		m := conversationModel()
		m.running = true
		thinking := streaming(m, llm.Event{Kind: llm.EventReasoning, Text: "let me see"})
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"ls"}`})

		forming, live := ansi.Strip(m.formingView()), thinking.liveText()

		if !slices.ContainsFunc(formingFrames, func(frame string) bool { return strings.Contains(forming, frame+" bash") }) {
			t.Errorf("forming:\n%s", forming)
		}
		if !slices.ContainsFunc(sparkFrames, func(frame string) bool { return strings.Contains(ansi.Strip(live), frame+" thinking…") }) {
			t.Errorf("reasoning:\n%s", live)
		}
	})

	t.Run("MK-03 a call written and not run is dotted while another runs, beating while the user is asked", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"sleep 9"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "bash", Text: `{"command":"echo b"}`})
		running, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash"}})
		asked, _ := running.(model).Update(interactionMsg{interaction: agent.Interaction{Approvals: []agent.ApprovalRequest{{Call: llm.ToolCall{ID: "c2", Name: "bash"}}}}, reply: make(chan agent.Reply, 1)})
		other, _ := running.(model).Update(interactionMsg{interaction: agent.Interaction{Approvals: []agent.ApprovalRequest{{Call: llm.ToolCall{ID: "elsewhere", Name: "bash"}}}}, reply: make(chan agent.Reply, 1)})

		if !strings.Contains(running.(model).formingView(), markQueued) || !strings.Contains(other.(model).formingView(), markQueued) {
			t.Errorf("waiting:\n%s\nasked about another:\n%s", running.(model).formingView(), other.(model).formingView())
		}
		if view := asked.(model).formingView(); !slices.ContainsFunc(askingFrames, func(frame string) bool { return strings.Contains(view, frame) }) {
			t.Errorf("asked:\n%s", view)
		}
	})

	t.Run("MK-04 the arc's place in a block never reaches the screen or what is copied", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		updated, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash", Args: []byte(`{"command":"sleep 9"}`)}, summary: "sleep 9"})
		m = updated.(model)

		screen, copied := screenOf(m), m.conversationContent()

		if strings.Contains(screen, markRunning) || strings.Contains(copied, markRunning) || !slices.ContainsFunc(arcFrames, func(frame string) bool { return strings.Contains(screen, frame+" bash") }) {
			t.Errorf("screen:\n%s", screen)
		}
	})
}
