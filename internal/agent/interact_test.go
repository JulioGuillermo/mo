package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | IN-01 | Happy    | The calls of a reply are asked at once, each run or denied as decided | P0      |
// | IN-02 | Happy    | Answers go back with their questions; a skipped one says so           | P0       |
// | IN-03 | Edge     | With nobody to answer, the model is told to decide itself            | P1       |
// | IN-04 | Error    | A malformed ask call is an error result and asks nobody              | P1       |
// | IN-05 | Happy    | An answered ask is shown as a call with the questions and answers     | P0       |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func askCall(id string, questions ...tools.Question) llm.ToolCall {
	raw, _ := json.Marshal(map[string]any{"questions": questions})
	return llm.ToolCall{ID: id, Name: tools.AskName, Args: raw}
}

func TestInteraction(t *testing.T) {
	ctx := context.Background()

	t.Run("IN-01 the calls of a reply are asked at once, each run or denied as decided", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				toolCall("a", "bash", `{"command":"echo one"}`),
				toolCall("b", "read", `{"path":"a.txt"}`),
				toolCall("c", "bash", `{"command":"echo two"}`),
			}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider, "bash")
		var asked []Interaction
		hooks := Hooks{Interact: func(_ context.Context, interaction Interaction) Reply {
			asked = append(asked, interaction)
			return Reply{Decisions: []Decision{Deny, Allow}}
		}}

		_ = agent.Run(ctx, "go", hooks)

		results := agent.History[2:5]
		if len(asked) != 1 || len(asked[0].Approvals) != 2 || asked[0].Approvals[1].Summary != "echo two" {
			t.Fatalf("asked = %+v", asked)
		}
		if results[0].Text != deniedResult || strings.TrimSpace(results[1].Text) != "hello" || strings.TrimSpace(results[2].Text) != "two" {
			t.Errorf("results = %+v", results)
		}
	})

	t.Run("IN-02 answers go back with their questions; a skipped one says so", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{askCall("q", tools.Question{Text: "Which db?", Options: []string{"Postgres", "SQLite"}}, tools.Question{Text: "Port?"})}},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent := newAgent(t, provider)
		var questions []tools.Question
		hooks := Hooks{Interact: func(_ context.Context, interaction Interaction) Reply {
			questions = interaction.Questions
			return Reply{Answers: []string{"Postgres", ""}}
		}}

		_ = agent.Run(ctx, "set up", hooks)

		result := agent.History[2]
		if len(questions) != 2 || questions[0].Options[1] != "SQLite" || result.IsError || result.Text != "Q: Which db?\nA: Postgres\n\nQ: Port?\nA: "+noAnswer {
			t.Errorf("questions = %+v result = %+v", questions, result)
		}
	})

	t.Run("IN-03 with nobody to answer, the model is told to decide itself", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{askCall("q", tools.Question{Text: "Which db?"})}},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent := newAgent(t, provider)

		_ = agent.Run(ctx, "set up", Hooks{})

		if agent.History[2].Text != nobodyToAnswer {
			t.Errorf("result = %q", agent.History[2].Text)
		}
	})

	t.Run("IN-04 a malformed ask call is an error result and asks nobody", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("q", tools.AskName, `{"questions":[]}`)}},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent := newAgent(t, provider)
		asked := false
		hooks := Hooks{Interact: func(context.Context, Interaction) Reply { asked = true; return Reply{} }}

		_ = agent.Run(ctx, "set up", hooks)

		if asked || !agent.History[2].IsError || !strings.Contains(agent.History[2].Text, "1 to 4 questions") {
			t.Errorf("asked = %v result = %+v", asked, agent.History[2])
		}
	})

	t.Run("IN-05 an answered ask is shown as a call with the questions and answers", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{askCall("q", tools.Question{Text: "Which db?"})}},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent := newAgent(t, provider)
		var started, done []string
		hooks := Hooks{
			Interact:    func(context.Context, Interaction) Reply { return Reply{Answers: []string{"Postgres"}} },
			OnToolStart: func(call llm.ToolCall, _ string) { started = append(started, call.Name) },
			OnToolDone:  func(call llm.ToolCall, output string, _ bool) { done = append(done, output) },
		}

		_ = agent.Run(ctx, "set up", hooks)

		if len(started) != 1 || started[0] != tools.AskName || len(done) != 1 || done[0] != "Q: Which db?\nA: Postgres" {
			t.Errorf("started = %q done = %q", started, done)
		}
	})
}
