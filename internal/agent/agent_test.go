package agent

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | AG-01 | Happy    | Runs the requested tool, sends the result back, ends on text        | P0       |
// | AG-02 | Security | A call that needs confirmation is denied without an approver       | P0       |
// | AG-03 | Happy    | An approved call runs                                               | P0       |
// | AG-04 | Edge     | After a cancel every pending tool call still gets a result         | P0       |
// | AG-05 | Error    | A provider error stops the run and is returned                     | P1       |
// | AG-06 | Security | A write outside the working directory asks even when not listed    | P0       |
// | AG-07 | Security | AllowAlways stops asking for that tool, but not outside the workdir | P0       |
// | AG-08 | Error    | A call with cut-off arguments gets its error, without running      | P1       |
// | AG-09 | Happy    | RewritePrompt changes the prompt before it enters the history      | P1       |
// | AG-10 | Security | BlockToolCall stops a call before anyone is asked to approve it    | P0       |
// | AG-11 | Happy    | Reasoning stays in the history but goes back only with KeepReasoning | P0     |

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

type scriptedProvider struct {
	replies  []llm.Message
	err      error
	requests []llm.Request
}

func (p *scriptedProvider) Stream(_ context.Context, req llm.Request, _ func(llm.Event)) (llm.Response, error) {
	p.requests = append(p.requests, req)
	if p.err != nil {
		return llm.Response{}, p.err
	}
	reply := p.replies[0]
	p.replies = p.replies[1:]
	return llm.Response{Message: reply}, nil
}

func toolCall(id, name, args string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func newAgent(t *testing.T, provider llm.Provider, confirm ...string) *Agent {
	t.Helper()
	agent, _ := newAgentIn(t, provider, confirm...)
	return agent
}

func newAgentIn(t *testing.T, provider llm.Provider, confirm ...string) (*Agent, string) {
	t.Helper()
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{
		Provider:     provider,
		Tools:        tools.NewRegistry(tools.Env{WorkDir: workDir}, tools.Options{MaxOutput: 1000}),
		ConfirmTools: confirm,
	}
	return agent, workDir
}

func TestRun(t *testing.T) {
	t.Run("AG-01 runs the requested tool, sends the result back, ends on text", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "it says hello"},
		}}
		agent := newAgent(t, provider)

		err := agent.Run(context.Background(), "what is in a.txt?", Hooks{})

		if err != nil {
			t.Fatal(err)
		}
		if len(provider.requests) != 2 {
			t.Fatalf("requests = %d", len(provider.requests))
		}
		result := agent.History[2]
		if result.Role != llm.RoleTool || result.ToolCallID != "c1" || result.Text != "hello\n" || result.IsError {
			t.Errorf("tool result = %+v", result)
		}
		if len(agent.History) != 4 || agent.History[3].Text != "it says hello" {
			t.Errorf("history = %+v", agent.History)
		}
	})

	t.Run("AG-02 a call that needs confirmation is denied without an approver", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "rm", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent, workDir := newAgentIn(t, provider, "rm")

		if err := agent.Run(context.Background(), "delete a.txt", Hooks{}); err != nil {
			t.Fatal(err)
		}

		result := agent.History[2]
		if !result.IsError || result.Text != deniedResult {
			t.Errorf("tool result = %+v", result)
		}
		if _, err := os.Stat(filepath.Join(workDir, "a.txt")); err != nil {
			t.Errorf("a.txt was deleted: %v", err)
		}
	})

	t.Run("AG-03 an approved call runs", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "bash", `{"command":"echo hi"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider, "bash")
		var asked string
		hooks := Hooks{Approve: func(_ context.Context, request ApprovalRequest) Decision {
			asked = request.Summary
			return Allow
		}}

		if err := agent.Run(context.Background(), "say hi", hooks); err != nil {
			t.Fatal(err)
		}

		if asked != "echo hi" || agent.History[2].Text != "hi\n" {
			t.Errorf("asked = %q result = %+v", asked, agent.History[2])
		}
	})

	t.Run("AG-04 after a cancel every pending tool call still gets a result", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				toolCall("c1", "bash", `{"command":"echo one"}`),
				toolCall("c2", "bash", `{"command":"echo two"}`),
			}},
		}}
		agent := newAgent(t, provider, "bash")
		ctx, cancel := context.WithCancel(context.Background())
		hooks := Hooks{Approve: func(context.Context, ApprovalRequest) Decision {
			cancel()
			return Deny
		}}

		err := agent.Run(ctx, "go", hooks)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		results := agent.History[2:]
		if len(results) != 2 || results[0].ToolCallID != "c1" || results[1].ToolCallID != "c2" {
			t.Fatalf("results = %+v", results)
		}
		if results[0].Text != cancelledResult || results[1].Text != cancelledResult {
			t.Errorf("results = %q, %q", results[0].Text, results[1].Text)
		}
	})

	t.Run("AG-05 a provider error stops the run and is returned", func(t *testing.T) {
		provider := &scriptedProvider{err: errors.New("503 Service Unavailable")}
		agent := newAgent(t, provider)

		err := agent.Run(context.Background(), "hi", Hooks{})

		if err == nil || err.Error() != "503 Service Unavailable" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("AG-06 a write outside the working directory asks even when not listed", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "x.txt")
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				toolCall("c1", "write", `{"path":"inside.txt","content":"ok"}`),
				toolCall("c2", "write", `{"path":"`+outside+`","content":"no"}`),
			}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider)
		var asked []ApprovalRequest
		hooks := Hooks{Approve: func(_ context.Context, request ApprovalRequest) Decision {
			asked = append(asked, request)
			return Deny
		}}

		if err := agent.Run(context.Background(), "write", hooks); err != nil {
			t.Fatal(err)
		}

		if len(asked) != 1 || asked[0].Call.ID != "c2" || asked[0].Warning == "" {
			t.Fatalf("asked = %+v", asked)
		}
		if _, err := os.Stat(outside); err == nil {
			t.Error("the outside file was written")
		}
	})

	t.Run("AG-07 AllowAlways stops asking for that tool, but not outside the workdir", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "gone")
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "mkdir", `{"path":"one"}`)}},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c2", "mkdir", `{"path":"two"}`)}},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c3", "mkdir", `{"path":"`+outside+`"}`)}},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c4", "mkdir", `{"path":"`+outside+`"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider, "mkdir")
		var asked []string
		hooks := Hooks{Approve: func(_ context.Context, request ApprovalRequest) Decision {
			asked = append(asked, request.Call.ID)
			return AllowAlways
		}}

		if err := agent.Run(context.Background(), "mkdirs", hooks); err != nil {
			t.Fatal(err)
		}

		if strings.Join(asked, ",") != "c1,c3,c4" {
			t.Errorf("asked = %v", asked)
		}
	})

	t.Run("AG-08 a call with cut-off arguments gets its error, without running", func(t *testing.T) {
		call := toolCall("c1", "write", `{}`)
		call.ArgsError = "cut off"
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
			{Role: llm.RoleAssistant, Text: "retrying"},
		}}
		agent := newAgent(t, provider)

		if err := agent.Run(context.Background(), "write", Hooks{}); err != nil {
			t.Fatal(err)
		}

		if result := agent.History[2]; !result.IsError || result.Text != "cut off" {
			t.Errorf("result = %+v", result)
		}
	})
}

func TestPluginHooks(t *testing.T) {
	t.Run("AG-09 RewritePrompt changes the prompt before it enters the history", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}}}
		agent := newAgent(t, provider)
		agent.RewritePrompt = func(_ context.Context, prompt string) string { return prompt + " (be brief)" }

		if err := agent.Run(context.Background(), "explain", Hooks{}); err != nil {
			t.Fatal(err)
		}

		if agent.History[0].Text != "explain (be brief)" {
			t.Errorf("first = %q", agent.History[0].Text)
		}
	})

	t.Run("AG-10 BlockToolCall stops a call before anyone is asked to approve it", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "bash", `{"command":"git push --force"}`)}},
			{Role: llm.RoleAssistant, Text: "understood"},
		}}
		agent := newAgent(t, provider, "bash")
		agent.BlockToolCall = func(context.Context, llm.ToolCall) string { return "blocked by plugin guard: no force pushes" }
		asked := false
		var shown string
		hooks := Hooks{
			Approve:    func(context.Context, ApprovalRequest) Decision { asked = true; return Allow },
			OnToolDone: func(_ llm.ToolCall, output string, isError bool) { shown = output },
		}

		if err := agent.Run(context.Background(), "push", hooks); err != nil {
			t.Fatal(err)
		}

		if result := agent.History[2]; asked || !result.IsError || !strings.Contains(result.Text, "no force pushes") {
			t.Errorf("asked = %v result = %+v", asked, result)
		}
		if !strings.Contains(shown, "no force pushes") {
			t.Errorf("the block was not shown: %q", shown)
		}
	})
}

func TestReasoningSentBack(t *testing.T) {
	t.Run("AG-11 reasoning stays in the history but goes back only with KeepReasoning", func(t *testing.T) {
		sentWith := func(keep bool) (string, string) {
			provider := &scriptedProvider{replies: []llm.Message{
				{Role: llm.RoleAssistant, Text: "one", Reasoning: "thought"},
				{Role: llm.RoleAssistant, Text: "two"},
			}}
			agent := newAgent(t, provider)
			agent.KeepReasoning = keep
			_ = agent.Run(context.Background(), "a", Hooks{})
			_ = agent.Run(context.Background(), "b", Hooks{})
			return provider.requests[1].Messages[1].Reasoning, agent.History[1].Reasoning
		}

		dropped, keptInHistory := sentWith(false)
		sent, _ := sentWith(true)

		if dropped != "" || keptInHistory != "thought" || sent != "thought" {
			t.Errorf("sent without keep = %q history = %q sent with keep = %q", dropped, keptInHistory, sent)
		}
	})
}

// onlyRead leaves the agent read alone, for tests whose windows are so
// small that the size of every tool's definition would decide them.
func onlyRead(t *testing.T, agent *Agent) {
	t.Helper()
	var others []string
	for _, name := range tools.BuiltinNames() {
		if name != "read" {
			others = append(others, name)
		}
	}
	agent.Tools = tools.NewRegistry(tools.Env{WorkDir: agent.Tools.WorkDir()}, tools.Options{MaxOutput: 1000, Disabled: others})
}
