package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | LT-01 | Happy    | A field is read from JSON cut short, escapes decoded, as far as it came | P0    |
// | LT-02 | Happy    | A call shows its name as soon as it comes, then its command forming  | P0       |
// | LT-03 | Happy    | A file being written shows its path, line count and last lines; tool_call shows the tool | P1 |
// | LT-04 | Edge     | A whole call the reply ended with waits in view until it runs, then goes | P0    |
// | LT-08 | Edge     | A call that ends streaming after the first started shows whole, waiting; the next reply starts anew | P0 |
// | LT-05 | Happy    | The spinner says what the agent waits on, compacting, and goes back to working | P1 |
// | LT-06 | Happy    | Each thing the agent does has its word and colour, in the spinner only | P1      |
// | LT-07 | Happy    | While llama-server reads the prompt, the state says how far, cache left out | P1   |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func streaming(m model, events ...llm.Event) model {
	for _, event := range events {
		updated, _ := m.Update(streamMsg(event))
		m = updated.(model)
	}
	return m
}

func piece(call int, name, text string) llm.Event {
	return llm.Event{Kind: llm.EventToolCall, Call: call, Name: name, Text: text}
}

func TestPartialJSON(t *testing.T) {
	t.Run("LT-01 a field is read from JSON cut short, escapes decoded, as far as it came", func(t *testing.T) {
		cases := []struct {
			json, key, want string
			found           bool
		}{
			{`{"command":"ls -la"}`, "command", "ls -la", true},
			{`{"command":"go te`, "command", "go te", true},
			{`{"content":"a\nb \"q\" café`, "content", "a\nb \"q\" café", true},
			{`{"content":"line\`, "content", "line", true},
			{`{"content":"x\u00`, "content", "x", true},
			{`{"path":"a.go","con`, "content", "", false},
			{`{"name":"bash","args":{"command":"make"`, "command", "make", true},
		}
		for _, c := range cases {
			got, found := partialString(c.json, c.key)
			if got != c.want || found != c.found {
				t.Errorf("%s in %q = %q, %v", c.key, c.json, got, found)
			}
		}
	})
}

func TestFormingCalls(t *testing.T) {
	t.Run("LT-02 a call shows its name as soon as it comes, then its command forming", func(t *testing.T) {
		m := conversationModel()
		m.running = true

		named := streaming(m, piece(0, "bash", ""))
		forming := streaming(named, piece(0, "", `{"command":"go test ./`), piece(0, "", `.. && echo ok`))

		if !strings.Contains(steady(shown2(named)), "◜ bash") || !strings.Contains(shown2(forming), "go test ./.. && echo ok") {
			t.Errorf("named:\n%s\nforming:\n%s", shown2(named), shown2(forming))
		}
	})

	t.Run("LT-03 a file being written shows its path, line count and last lines; tool_call shows the tool", func(t *testing.T) {
		m := conversationModel()
		m.running = true
		var body strings.Builder
		for i := range 30 {
			body.WriteString("line " + string(rune('a'+i%26)) + "\\n")
		}

		writing := streaming(m, piece(0, "write", `{"path":"notes.md","content":"`+body.String()+`last line`))
		wrapped := streaming(m, piece(0, "tool_call", `{"name":"edit","args":{"path":"a.go","new":"x := 1`))

		view := steady(shown2(writing))
		if !strings.Contains(view, "◜ write notes.md") || !strings.Contains(view, "writing · 31 lines") || !strings.Contains(view, "last line") || strings.Contains(view, "line a\n") && strings.Count(view, "line ") > 13 {
			t.Errorf("writing:\n%s", view)
		}
		if !strings.Contains(steady(shown2(wrapped)), "◜ edit a.go") || !strings.Contains(shown2(wrapped), "x := 1") {
			t.Errorf("wrapped:\n%s", shown2(wrapped))
		}
	})

	t.Run("LT-08 a call that ends streaming after the first started shows whole, waiting; the next reply starts anew", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"sleep 12"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "bash", Text: `{"comm`})
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash"}})
		shown, _ := started.(model).Update(streamEndMsg{})
		m = streaming(shown.(model), llm.Event{Kind: llm.EventToolCall, Call: 1, Text: `and":"echo SECOND"}`})
		waiting := shown2(m)
		second, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c2", Name: "bash"}})
		next := streaming(second.(model), llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "d1", Name: "read", Text: `{"path":"x"}`})

		if !strings.Contains(waiting, "echo SECOND") || !strings.Contains(waiting, "waits for the call before it") {
			t.Errorf("waiting:\n%s", waiting)
		}
		if len(next.forming) != 1 || next.forming[0].name != "read" {
			t.Errorf("next reply's calls %+v", next.forming)
		}
	})

	t.Run("LT-04 a whole call the reply ended with waits in view until it runs, then goes", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"sleep 12"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "bash", Text: `{"command":"echo SECOND"}`})
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash"}})

		ended, _ := started.(model).Update(streamEndMsg{})
		waiting := shown2(ended.(model))
		second, _ := ended.(model).Update(toolStartMsg{call: llm.ToolCall{ID: "c2", Name: "bash"}})
		finished, _ := second.(model).finishRun(nil)

		if !strings.Contains(waiting, "echo SECOND") || !strings.Contains(waiting, "waits for the call before it") {
			t.Errorf("while the first runs:\n%s", waiting)
		}
		if strings.Contains(shown2(second.(model)), "waits for the call before it") || len(finished.(model).forming) != 0 {
			t.Errorf("once it runs:\n%s", shown2(second.(model)))
		}
	})
}

// shown2 is the conversation as drawn, live part included.
func shown2(m model) string {
	return ansi.Strip(m.conversation(40).View())
}

func TestActivity(t *testing.T) {
	t.Run("LT-05 the spinner says what the agent waits on, compacting, and goes back to working", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}

		compacting, _ := m.Update(activityMsg("compacting the conversation"))
		back, _ := compacting.(model).Update(activityMsg(""))
		done, _ := compacting.(model).finishRun(nil)

		if !strings.Contains(ansi.Strip(compacting.(model).bottom()), "compacting") || !strings.Contains(ansi.Strip(back.(model).bottom()), "waiting") || done.(model).activity != "" {
			t.Errorf("compacting:\n%s\nback:\n%s", ansi.Strip(compacting.(model).bottom()), ansi.Strip(back.(model).bottom()))
		}
	})
}

func TestStates(t *testing.T) {
	t.Run("LT-06 each thing the agent does has its word and colour, in the spinner only", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		colors := statePalette(true)
		cases := []struct {
			name  string
			setup func(model) model
			word  string
			tint  any
		}{
			{"waiting", func(m model) model { return m }, "waiting", colors.waiting},
			{"reasoning", func(m model) model { return streaming(m, llm.Event{Kind: llm.EventReasoning, Text: "hmm"}) }, "reasoning", colors.reasoning},
			{"writing", func(m model) model { return streaming(m, llm.Event{Kind: llm.EventText, Text: "Sure"}) }, "writing", colors.writing},
			{"reading", func(m model) model { return streaming(m, piece(0, "read", `{"path":"a.go"}`)) }, "reading", colors.reading},
			{"changing", func(m model) model { return streaming(m, piece(0, "edit", `{"path":"a.go"}`)) }, "changing", colors.changing},
			{"bash", func(m model) model { return streaming(m, piece(0, "bash", `{"command":"ls"}`)) }, "changing", colors.changing},
			{"prompt", func(m model) model {
				return streaming(m, llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 600, Total: 1000}})
			}, "prompt", colors.prompt},
			{"compacting", func(m model) model {
				updated, _ := m.Update(activityMsg("compacting the conversation"))
				return updated.(model)
			}, "compacting", colors.compacting},
		}
		for _, c := range cases {
			got := c.setup(m).currentState()
			if got.word != c.word || got.tint != c.tint {
				t.Errorf("%s: word = %q", c.name, got.word)
			}
			spinner, _ := c.setup(m).upperLayout()
			if !strings.Contains(ansi.Strip(spinner), c.word) {
				t.Errorf("%s: the spinner does not say %q", c.name, c.word)
			}
			if strings.Contains(ansi.Strip(c.setup(m).statusLine()), c.word) {
				t.Errorf("%s: the status line says %q too", c.name, c.word)
			}
		}
	})

	t.Run("LT-07 while llama-server reads the prompt, the state says how far, cache left out", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}

		reading := streaming(m, llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 5000, Total: 9000, Cached: 1000}}).currentState()
		answering := streaming(streaming(m, llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 9000, Total: 9000}}), llm.Event{Kind: llm.EventText, Text: "Hi"}).currentState()

		if reading.detail != "50% · 4.0k/8.0k · 1.0k cached" || answering.word != "writing" {
			t.Errorf("reading %q answering %q", reading.detail, answering.word)
		}
	})
}
