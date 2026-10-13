package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | QU-01 | Happy    | A message queued while the agent works reaches the model at its next step | P0  |
// | QU-02 | Happy    | Queued after the last step, it starts a turn of its own              | P1       |

import (
	"context"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestQueue(t *testing.T) {
	t.Run("QU-01 a message queued while the agent works reaches the model at its next step", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "done, and in Spanish too"},
		}}
		agent := newAgent(t, provider)
		hooks := Hooks{OnToolDone: func(llm.ToolCall, string, bool) { agent.Queue("answer in Spanish too") }}

		_ = agent.Run(context.Background(), "read a.txt", hooks)

		second := provider.requests[1].Messages
		last := second[len(second)-1]
		if last.Role != llm.RoleUser || last.Text != "answer in Spanish too" || agent.HasQueued() {
			t.Errorf("last sent = %+v", last)
		}
	})

	t.Run("QU-02 queued after the last step, it starts a turn of its own", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "sure"}}}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "hi"}, {Role: llm.RoleAssistant, Text: "hello"}}
		agent.Queue("one more thing")

		err := agent.RunQueued(context.Background(), Hooks{})

		sent := provider.requests[0].Messages
		if err != nil || sent[len(sent)-1].Text != "one more thing" || agent.History[len(agent.History)-1].Text != "sure" {
			t.Errorf("err = %v sent = %+v", err, sent)
		}
	})
}
