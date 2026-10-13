package agent

// Test Traceability Table
// | ID    | Category | Description                                                         | Priority |
// | DR-01 | Happy    | With strategy drop, the oldest turns go until the context fits      | P0       |
// | DR-02 | Edge     | What is left starts at a user message, so no tool result is orphaned | P0       |
// | DR-03 | Edge     | The turn in progress is never dropped; the user is told             | P1       |
// | DR-04 | Happy    | Dropping asks the model nothing                                     | P1       |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func turn(prompt string, resultSize int) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Text: prompt},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c-"+prompt, "read", `{"path":"a.txt"}`)}},
		{Role: llm.RoleTool, ToolCallID: "c-" + prompt, Text: strings.Repeat("x", resultSize)},
		{Role: llm.RoleAssistant, Text: "done " + prompt},
	}
}

func droppingAgent(t *testing.T, provider *scriptedProvider) *Agent {
	t.Helper()
	agent := newAgent(t, provider)
	agent.Window, agent.CompactAt, agent.Strategy = 2000, 0.75, StrategyDrop
	agent.System = ""
	return agent
}

func TestDropOldest(t *testing.T) {
	t.Run("DR-01 with strategy drop, the oldest turns go until the context fits", func(t *testing.T) {
		agent := droppingAgent(t, &scriptedProvider{})
		for _, prompt := range []string{"one", "two", "three", "four"} {
			agent.History = append(agent.History, turn(prompt, 1200)...)
		}
		var notice string

		agent.dropOldest(Hooks{OnNotice: func(text string) { notice = text }})

		target := int(float64(agent.Window) * agent.CompactAt * dropTargetShare)
		if agent.ContextUsed() > target || len(agent.History) == 16 {
			t.Errorf("used = %d target = %d messages = %d", agent.ContextUsed(), target, len(agent.History))
		}
		if agent.History[len(agent.History)-1].Text != "done four" || !strings.Contains(notice, "dropped the") {
			t.Errorf("last = %+v notice = %q", agent.History[len(agent.History)-1], notice)
		}
	})

	t.Run("DR-02 what is left starts at a user message, so no tool result is orphaned", func(t *testing.T) {
		agent := droppingAgent(t, &scriptedProvider{})
		agent.History = append(turn("one", 3000), turn("two", 100)...)

		agent.dropOldest(Hooks{})

		if agent.History[0].Role != llm.RoleUser || agent.History[0].Text != "two" {
			t.Errorf("history starts with %+v", agent.History[0])
		}
	})

	t.Run("DR-03 the turn in progress is never dropped; the user is told", func(t *testing.T) {
		agent := droppingAgent(t, &scriptedProvider{})
		agent.History = turn("only", 9000)
		var notice string

		agent.dropOldest(Hooks{OnNotice: func(text string) { notice = text }})

		if len(agent.History) != 4 || !strings.Contains(notice, "turn in progress") {
			t.Errorf("messages = %d notice = %q", len(agent.History), notice)
		}
	})

	t.Run("DR-04 dropping asks the model nothing", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}}}
		agent := droppingAgent(t, provider)
		agent.History = append(turn("one", 4000), turn("two", 4000)...)

		if err := agent.Run(context.Background(), "three", Hooks{}); err != nil {
			t.Fatal(err)
		}

		if len(provider.requests) != 1 || provider.requests[0].Messages[0].Text == "one" {
			t.Errorf("requests = %d first = %q", len(provider.requests), provider.requests[0].Messages[0].Text)
		}
	})
}
