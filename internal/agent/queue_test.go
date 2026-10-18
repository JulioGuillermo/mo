package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | QU-01 | Happy    | A message queued while the agent works reaches the model at its next step | P0  |
// | QU-02 | Happy    | Queued after the last step, it starts a turn of its own              | P1       |
// | QU-03 | Happy    | OnQueued says how many queued messages the model now reads           | P1       |
// | QU-04 | Happy    | Several queued while the agent works reach the model as one message  | P0       |
// | QU-05 | Happy    | Several queued after the last step are one new message of the user's | P0       |
// | QU-06 | Edge     | Queued after a user message they go in it, never as a second one     | P0       |
// | QU-07 | Edge     | Queued after a note of mo's, they go before it and stay the prompt   | P0       |

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

	t.Run("QU-03 OnQueued says how many queued messages the model now reads", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "sure"}}}
		agent := newAgent(t, provider)
		agent.Queue("one")
		agent.Queue("two")
		var told []int

		_ = agent.RunQueued(context.Background(), Hooks{OnQueued: func(count int) { told = append(told, count) }})

		if len(told) != 1 || told[0] != 2 {
			t.Errorf("told = %v", told)
		}
	})

	t.Run("QU-04 several queued while the agent works reach the model as one message", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider)
		hooks := Hooks{OnToolDone: func(llm.ToolCall, string, bool) {
			agent.Queue("answer in Spanish")
			agent.Queue("and keep it short")
		}}

		_ = agent.Run(context.Background(), "read a.txt", hooks)

		second := provider.requests[1].Messages
		last, before := second[len(second)-1], second[len(second)-2]
		if last.Role != llm.RoleUser || last.Text != "answer in Spanish\n\nand keep it short" || before.Role == llm.RoleUser {
			t.Errorf("last sent = %+v before = %+v", last, before)
		}
	})

	t.Run("QU-05 several queued after the last step are one new message of the user's", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "sure"}}}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "hi"}, {Role: llm.RoleAssistant, Text: "hello"}}
		agent.Queue("one")
		agent.Queue("two")
		agent.Queue("three")

		_ = agent.RunQueued(context.Background(), Hooks{})

		sent := provider.requests[0].Messages
		if len(sent) != 3 || sent[2].Text != "one\n\ntwo\n\nthree" || sent[2].Role != llm.RoleUser {
			t.Errorf("sent = %+v", sent)
		}
	})

	t.Run("QU-06 queued after a user message they go in it, never as a second one", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "sure"}}}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "hi"}}
		agent.Queue("and this")

		_ = agent.RunQueued(context.Background(), Hooks{})

		sent := provider.requests[0].Messages
		if len(sent) != 1 || sent[0].Text != "hi\n\nand this" {
			t.Errorf("sent = %+v", sent)
		}
	})

	t.Run("QU-07 queued after a note of mo's, they go before it and stay the prompt", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "sure"}}}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "hi"}, {Role: llm.RoleAssistant, Text: "hello"}, {Role: llm.RoleUser, Text: moNotePrefix + "a job ended"}}
		agent.Queue("next thing")

		_ = agent.RunQueued(context.Background(), Hooks{})

		sent := provider.requests[0].Messages
		last := sent[len(sent)-1]
		if len(sent) != 3 || last.Text != "next thing\n\n"+moNotePrefix+"a job ended" || !isPrompt(last) {
			t.Errorf("sent = %+v", sent)
		}
	})
}
