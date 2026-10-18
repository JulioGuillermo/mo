package agent

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CC-01 | Happy    | A compaction gives the whole history as it was, then the one it left     | P0       |
// | CC-02 | Happy    | Dropping the oldest turns gives the whole history as it was              | P0       |
// | CC-03 | Edge     | A compaction that fails, or a drop that drops nothing, gives nothing     | P0       |
// | CC-04 | Edge     | What the hook was given is a copy: the agent's own changes do not reach it | P1     |

import (
	"context"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestContextCut(t *testing.T) {
	ctx := context.Background()

	t.Run("CC-01 a compaction gives the whole history as it was, then the one it left", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "- all of it"}}})
		agent.Window = 100000
		agent.History = append(longHistory(), llm.Message{Role: llm.RoleUser, Text: "small last turn"}, llm.Message{Role: llm.RoleAssistant, Text: "done"})
		var order []string
		var whole []llm.Message
		var after []llm.Message

		err := agent.CompactWith(ctx, Hooks{
			OnContextCut: func(before []llm.Message) { order, whole = append(order, "cut"), before },
			OnCompact:    func(int, int) { order = append(order, "compact") },
			OnStep:       func(history []llm.Message) { order = append(order, "step"); after = history },
		}, "")

		if err != nil || len(whole) != 6 || whole[0].Text != "fix the build" || whole[5].Text != "done" {
			t.Fatalf("err = %v whole = %+v", err, whole)
		}
		if len(order) != 3 || order[0] != "cut" || order[1] != "compact" || order[2] != "step" || len(after) != 1 {
			t.Errorf("order = %v after = %+v", order, after)
		}
	})

	t.Run("CC-02 dropping the oldest turns gives the whole history as it was", func(t *testing.T) {
		agent := droppingAgent(t, &scriptedProvider{})
		for _, prompt := range []string{"one", "two", "three", "four"} {
			agent.History = append(agent.History, turn(prompt, 1200)...)
		}
		var whole []llm.Message
		steps := 0

		agent.dropOldest(Hooks{OnContextCut: func(before []llm.Message) { whole = before }, OnStep: func([]llm.Message) { steps++ }})

		if len(whole) != 16 || whole[0].Text != "one" || len(agent.History) >= 16 || steps != 1 {
			t.Errorf("whole = %d messages, history = %d, steps = %d", len(whole), len(agent.History), steps)
		}
	})

	t.Run("CC-03 a compaction that fails, or a drop that drops nothing, gives nothing", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "  "}}})
		agent.History = longHistory()
		given := 0
		hooks := Hooks{OnContextCut: func([]llm.Message) { given++ }}

		err := agent.Compact(ctx, hooks)
		only := droppingAgent(t, &scriptedProvider{})
		only.History = turn("one", 6000)
		only.dropOldest(hooks)

		if err == nil || given != 0 {
			t.Errorf("err = %v given %d times", err, given)
		}
	})

	t.Run("CC-04 what the hook was given is a copy: the agent's own changes do not reach it", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "- all of it"}}})
		agent.History = longHistory()
		var whole []llm.Message

		_ = agent.CompactWith(ctx, Hooks{OnContextCut: func(before []llm.Message) { whole = before }}, "")
		agent.History[0].Text = "changed"

		if len(whole) != 4 || whole[0].Text != "fix the build" {
			t.Errorf("whole = %+v", whole)
		}
	})
}
