package agent

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | IN-01 | Happy    | A prompt changed on its way keeps what was typed as Input, shown as typed   | P0       |
// | IN-02 | Edge     | A prompt sent as typed has no Input; one with notes of mo's after it has    | P1       |
// | IN-03 | Security | Typed text that looks like mo's own, a skill reference or a [mo] note, shows as typed | P0 |
// | IN-04 | Happy    | A prompt hook's rewrite reaches the model; the user's text stays as typed   | P0       |
// | IN-05 | Happy    | Queued messages, alone or merged during a turn, keep what each one was typed | P0      |
// | IN-06 | Security | Input is saved only when set and never reaches the provider                 | P0       |
// | IN-07 | Edge     | A message from before Input shows as it did                                 | P1       |
// | IN-08 | Edge     | An image dropped for a model without vision keeps the text as typed          | P2       |
// | IN-09 | Happy    | Queued after a prompt, they join it and it shows both as typed              | P0       |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func answered(t *testing.T) (*Agent, *scriptedProvider) {
	t.Helper()
	provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}, {Role: llm.RoleAssistant, Text: "ok"}}}
	return newAgent(t, provider), provider
}

func TestTypedInput(t *testing.T) {
	t.Run("IN-01 a prompt changed on its way keeps what was typed as Input, shown as typed", func(t *testing.T) {
		agent, _ := answered(t)
		typed := "look at [image #1] with $pdf"
		sent := "look at /tmp/paste.png with [SKILL REF: pdf]\n\n[mo] load pdf"

		_ = agent.RunTyped(context.Background(), sent, typed, Hooks{})

		prompt := agent.History[0]
		if prompt.Text != sent || prompt.Input != typed || PromptText(prompt) != typed {
			t.Errorf("prompt = %+v", prompt)
		}
	})

	t.Run("IN-02 a prompt sent as typed has no Input; one with notes of mo's after it has", func(t *testing.T) {
		plain, _ := answered(t)
		noted, _ := answered(t)

		_ = plain.Run(context.Background(), "hello", Hooks{})
		_ = noted.RunTyped(context.Background(), "hello\n\n[mo] a note", "hello", Hooks{})

		if plain.History[0].Input != "" || noted.History[0].Input != "hello" || PromptText(noted.History[0]) != "hello" {
			t.Errorf("plain = %+v noted = %+v", plain.History[0], noted.History[0])
		}
	})

	t.Run("IN-03 typed text that looks like mo's own, a skill reference or a [mo] note, shows as typed", func(t *testing.T) {
		for _, typed := range []string{"what is [SKILL REF: pdf]?", "a log:\n\n[mo] something"} {
			agent, provider := answered(t)

			_ = agent.Run(context.Background(), typed, Hooks{})

			if got := PromptText(agent.History[0]); got != typed || provider.requests[0].Messages[0].Text != typed {
				t.Errorf("%q shown as %q", typed, got)
			}
		}
	})

	t.Run("IN-04 a prompt hook's rewrite reaches the model; the user's text stays as typed", func(t *testing.T) {
		agent, provider := answered(t)
		agent.RewritePrompt = func(_ context.Context, prompt string) string { return strings.ToUpper(prompt) }

		_ = agent.Run(context.Background(), "make it loud", Hooks{})

		if provider.requests[0].Messages[0].Text != "MAKE IT LOUD" || PromptText(agent.History[0]) != "make it loud" {
			t.Errorf("sent %q shown %q", provider.requests[0].Messages[0].Text, PromptText(agent.History[0]))
		}
	})

	t.Run("IN-05 queued messages, alone or merged during a turn, keep what each one was typed", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider)
		hooks := Hooks{OnToolDone: func(llm.ToolCall, string, bool) {
			agent.QueueTyped(Queued{Text: "also [SKILL REF: pdf]\n\n[mo] load pdf", Typed: "also $pdf"})
			agent.Queue("and [SKILL REF: x] as written")
		}}
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "first"}, {Role: llm.RoleAssistant, Text: "ok"}}
		agent.Queue("[mo] looks like a note")

		_ = agent.RunQueued(context.Background(), hooks)

		want := "[mo] looks like a note"
		if got := PromptText(agent.History[2]); got != want {
			t.Errorf("queued alone shown %q", got)
		}
		prompts := PromptsIn(agent.History)
		want = "also $pdf\n\nand [SKILL REF: x] as written"
		if got := prompts[len(prompts)-1].Text; got != want {
			t.Errorf("queued during the turn shown %q, want %q", got, want)
		}
	})

	t.Run("IN-09 queued after a prompt, they join it and it shows both as typed", func(t *testing.T) {
		agent, _ := answered(t)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "read [SKILL REF: pdf]\n\n[mo] load pdf", Input: "read $pdf"}}
		agent.QueueTyped(Queued{Text: "then /tmp/a.png", Typed: "then [image a.png]"})

		_ = agent.RunQueued(context.Background(), Hooks{})

		if got := PromptText(agent.History[0]); got != "read $pdf\n\nthen [image a.png]" {
			t.Errorf("shown %q", got)
		}
	})

	t.Run("IN-06 Input is saved only when set and never reaches the provider", func(t *testing.T) {
		agent, provider := answered(t)
		_ = agent.RunTyped(context.Background(), "sent text", "typed text", Hooks{})

		withInput, _ := json.Marshal(agent.History[0])
		without, _ := json.Marshal(llm.Message{Role: llm.RoleUser, Text: "plain"})

		if !strings.Contains(string(withInput), `"Input":"typed text"`) || strings.Contains(string(without), "Input") {
			t.Errorf("with %s without %s", withInput, without)
		}
		if sent := provider.requests[0].Messages[0]; sent.Text != "sent text" || strings.Contains(sent.Text, "typed text") {
			t.Errorf("sent %+v", sent)
		}
	})

	t.Run("IN-07 a message from before Input shows as it did", func(t *testing.T) {
		old := llm.Message{Role: llm.RoleUser, Text: "use [SKILL REF: pdf]\n\n[mo] The user referenced skills: pdf."}

		if got := PromptText(old); got != "use $pdf" {
			t.Errorf("shown %q", got)
		}
	})

	t.Run("IN-08 an image dropped for a model without vision keeps the text as typed", func(t *testing.T) {
		agent, _ := answered(t)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "see this", Images: []llm.Image{{MIME: "image/png", Data: []byte{1}}}}}

		agent.withoutImages()

		if got := PromptText(agent.History[0]); got != "see this" || agent.History[0].Images != nil {
			t.Errorf("shown %q", got)
		}
	})
}
