package agent

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | AS-01 | Happy    | Aside has the main agent's model and history, without a reply still waiting for its results | P0 |
// | AS-02 | Security | A read-only aside blocks a call that changes something, without running it   | P0       |
// | AS-03 | Happy    | ChangesFrom lists the calls that ran well and change something, not reads nor failures | P0 |

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestAside(t *testing.T) {
	t.Run("AS-01 aside has the main agent's model and history, without a reply still waiting for its results", func(t *testing.T) {
		main := newAgent(t, &scriptedProvider{})
		main.Model, main.System = "qwen3", "be brief"
		main.History = []llm.Message{
			{Role: llm.RoleUser, Text: "fix it"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
		}

		aside := main.Aside(main.Tools, main.History, true)

		if aside.Model != "qwen3" || aside.System != "be brief" || len(aside.History) != 1 || aside.History[0].Text != "fix it" {
			t.Errorf("aside = %q %q %+v", aside.Model, aside.System, aside.History)
		}
		if len(main.History) != 2 {
			t.Errorf("main history changed: %+v", main.History)
		}
	})

	t.Run("AS-02 a read-only aside blocks a call that changes something, without running it", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "write", `{"path":"b.txt","content":"x"}`)}},
			{Role: llm.RoleAssistant, Text: "I can only read"},
		}}
		main, workDir := newAgentIn(t, provider)
		aside := main.Aside(main.Tools, nil, true)

		if err := aside.Run(context.Background(), "write b.txt", Hooks{}); err != nil {
			t.Fatal(err)
		}

		if result := aside.History[2]; !result.IsError || result.Text != readOnlyResult {
			t.Errorf("result = %+v", result)
		}
		if _, err := os.Stat(filepath.Join(workDir, "b.txt")); err == nil {
			t.Error("b.txt was written")
		}
	})

	t.Run("AS-03 ChangesFrom lists the calls that ran well and change something, not reads nor failures", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				toolCall("c1", "read", `{"path":"a.txt"}`),
				toolCall("c2", "write", `{"path":"b.txt","content":"x"}`),
				toolCall("c3", "rm", `{"path":"missing.txt"}`),
			}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		main := newAgent(t, provider)
		main.History = []llm.Message{{Role: llm.RoleUser, Text: "earlier"}, {Role: llm.RoleAssistant, Text: "ok"}}
		aside := main.Aside(main.Tools, main.History, false)
		start := len(aside.History)

		if err := aside.Run(context.Background(), "write b.txt", Hooks{}); err != nil {
			t.Fatal(err)
		}

		changes := aside.ChangesFrom(start)
		if len(changes) != 1 || changes[0] != "write "+aside.Tools.Summary(toolCall("c2", "write", `{"path":"b.txt","content":"x"}`)) {
			t.Errorf("changes = %q", changes)
		}
	})
}
