package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | HI-01 | Happy    | Prompts are the user's own, as typed, without mo's notes or summaries | P0      |
// | HI-02 | Happy    | Rewind drops the prompt and what follows, and the size is estimated again | P0  |

import (
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestHistory(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Text: summaryHeader + "we did X\n\n" + currentRequestHeader + "do Y"},
		{Role: llm.RoleAssistant, Text: "ok"},
		{Role: llm.RoleUser, Text: "look at a.png\n\n" + attachedNote + "\n/w/a.png"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "ls"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Text: "a.png"},
		{Role: llm.RoleUser, Text: moNotePrefix + "The images the tool calls above returned"},
		{Role: llm.RoleUser, Text: "thanks"},
	}

	t.Run("HI-01 prompts are the user's own, as typed, without mo's notes or summaries", func(t *testing.T) {
		agent := &Agent{History: history}

		prompts := agent.Prompts()

		if len(prompts) != 2 || prompts[0] != (Prompt{Index: 2, Text: "look at a.png"}) || prompts[1].Index != 6 || Summary(history[0]) != "we did X" {
			t.Errorf("prompts = %+v summary = %q", prompts, Summary(history[0]))
		}
	})

	t.Run("HI-02 rewind drops the prompt and what follows, and the size is estimated again", func(t *testing.T) {
		agent := &Agent{History: history, LastUsage: llm.Usage{InputTokens: 900}, historyAtUsage: 7}

		agent.Rewind(6)

		if len(agent.History) != 6 || agent.LastUsage.InputTokens != 0 || agent.historyAtUsage != 0 || len(history) != 7 {
			t.Errorf("history = %d usage = %+v", len(agent.History), agent.LastUsage)
		}
	})
}
