package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | LM-01 | Happy    | The context and speeds of a reply are counted from its stream as it comes   | P1       |
// | LM-02 | Happy    | The status line shows them marked ~ while it is written, the server's when it ends | P1 |

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func TestLiveMetrics(t *testing.T) {
	t.Run("LM-01 the context and speeds of a reply are counted from its stream as it comes", func(t *testing.T) {
		var live liveMetrics
		start := time.Unix(1000, 0)
		live.add(llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 0, Total: 5000, Cached: 1000}}, start)
		live.add(llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 3000, Total: 5000, Cached: 1000}}, start.Add(2*time.Second))
		reading := live.promptRate(start.Add(2 * time.Second))
		for i := range 41 {
			live.add(llm.Event{Kind: llm.EventText, Text: "tok "}, start.Add(3*time.Second+time.Duration(i)*50*time.Millisecond))
		}

		if reading != 1000 {
			t.Errorf("prompt read at %.0f t/s, want 1000", reading)
		}
		if got := live.generation(); got < 19.9 || got > 20.1 {
			t.Errorf("written at %.1f t/s, want 20", got)
		}
		if got := live.context(123); got != 5041 {
			t.Errorf("context = %d, want the prompt and what was written, 5041", got)
		}
		var anthropic liveMetrics
		anthropic.add(llm.Event{Kind: llm.EventText, Text: strings.Repeat("abcd", 50)}, start)
		if got := anthropic.context(1000); got != 1050 {
			t.Errorf("without the prompt's count, the context goes on from before: %d", got)
		}
	})

	t.Run("LM-02 the status line shows them marked ~ while it is written, the server's when it ends", func(t *testing.T) {
		m := sidebarModel(t, 140)
		m.session.Agent.Window = 10000
		m.running = true
		m.usage = llm.Usage{InputTokens: 2000, OutputTokens: 100}
		for i := range 30 {
			m.metrics.add(llm.Event{Kind: llm.EventText, Text: "x"}, time.Now().Add(time.Duration(i-30)*100*time.Millisecond))
		}
		show, colors := statusParts{prompt: true, generation: true, tokens: true}, statusPalette(true)
		shown := func() string { return ansi.Strip(m.contextPart(show, colors) + " " + m.speedPart(show, colors)) }
		writing := shown()
		updated, _ := m.Update(usageMsg(llm.Usage{InputTokens: 2000, OutputTokens: 130, GenerationRate: 9.5}))
		m = updated.(model)
		m.running = false
		done := shown()

		if !strings.Contains(writing, "~21%") || !strings.Contains(writing, "tg ~") {
			t.Errorf("while written: %q", writing)
		}
		if strings.Contains(done, "~") || !strings.Contains(done, "21%") || !strings.Contains(done, "tg 9.5") {
			t.Errorf("once it ends: %q", done)
		}
	})
}
