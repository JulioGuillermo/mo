package agent

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | AG-01 | Happy    | Runs the requested tool, sends the result back, ends on text        | P0       |
// | AG-02 | Security | A call that needs confirmation is denied without an approver       | P0       |
// | AG-03 | Happy    | An approved call runs                                               | P0       |
// | AG-04 | Edge     | After a cancel the calls that did not run go, none left without a result | P0 |
// | AG-05 | Error    | A provider error stops the run and is returned                     | P1       |
// | AG-06 | Security | A write outside the working directory asks even when not listed    | P0       |
// | AG-07 | Security | AllowAlways stops asking for that tool, but not outside the workdir | P0       |
// | AG-08 | Error    | A call with cut-off arguments gets its error, without running      | P1       |
// | AG-09 | Happy    | RewritePrompt changes the prompt before it enters the history      | P1       |
// | AG-10 | Security | BlockToolCall stops a call before anyone is asked to approve it    | P0       |
// | AG-11 | Happy    | Reasoning stays in the history but goes back only with KeepReasoning | P0     |
// | AG-12 | Edge     | An answer with no text and no call says so, shown and kept; one with text is left as is | P0 |
// | AG-13 | Happy    | A short limit is retried after the wait asked, saying so, then the turn goes on | P0 |
// | AG-14 | Edge     | A plan limit is not retried; a short one gives up after LimitRetries more tries | P0 |
// | AG-15 | Happy    | AfterToolCall changes what the model reads of a call, and what is shown | P0     |
// | AG-16 | Happy    | EndTurn hears each turn once, with its prompt, last answer and error  | P0       |
// | AG-17 | Happy    | StreamPiece hears every piece of text and reasoning as it comes       | P1       |

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

	t.Run("AG-04 after a cancel the calls that did not run go, so none is left without a result", func(t *testing.T) {
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
		if len(agent.History) != 1 || agent.History[0].Text != "go" {
			t.Errorf("history = %+v", agent.History)
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
				toolCall("c2", "write", `{"path":`+strconv.Quote(outside)+`,"content":"no"}`),
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
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c3", "mkdir", `{"path":`+strconv.Quote(outside)+`}`)}},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c4", "mkdir", `{"path":`+strconv.Quote(outside)+`}`)}},
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
	t.Run("AG-11 reasoning stays in the history; what goes back is none, this turn's or all, by KeepReasoning", func(t *testing.T) {
		// Turn one answers with a reasoning; turn two reasons, calls a tool and answers.
		sentWith := func(keep Reasoning) (older, midTurn, inHistory string) {
			provider := &scriptedProvider{replies: []llm.Message{
				{Role: llm.RoleAssistant, Text: "one", Reasoning: "old thought", Thinking: []byte(`["old"]`)},
				{Role: llm.RoleAssistant, Reasoning: "mid thought", Thinking: []byte(`["mid"]`), ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
				{Role: llm.RoleAssistant, Text: "two"},
			}}
			agent := newAgent(t, provider)
			agent.KeepReasoning = keep
			_ = agent.Run(context.Background(), "a", Hooks{})
			_ = agent.Run(context.Background(), "b", Hooks{})
			// The request after the tool result: a, one, b, the call, its result.
			last := provider.requests[2].Messages
			if (last[1].Reasoning == "") != (len(last[1].Thinking) == 0) || (last[3].Reasoning == "") != (len(last[3].Thinking) == 0) {
				t.Errorf("%q: reasoning and thinking blocks not sent together: %+v", keep, last)
			}
			return last[1].Reasoning, last[3].Reasoning, agent.History[1].Reasoning
		}

		for keep, want := range map[Reasoning][2]string{
			"":             {"", ""},
			ReasoningOff:   {"", ""},
			ReasoningTurn:  {"", "mid thought"},
			ReasoningAll:   {"old thought", "mid thought"},
			Reasoning("?"): {"", ""},
		} {
			older, midTurn, inHistory := sentWith(keep)
			if older != want[0] || midTurn != want[1] || inHistory != "old thought" {
				t.Errorf("%q: older turn %q, this turn %q, history %q; want %q", keep, older, midTurn, inHistory, want)
			}
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

func TestSilentAnswer(t *testing.T) {
	t.Run("AG-12 an answer with no text and no call says so, shown and kept; one with text is left as is", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Reasoning: "let me think", Text: "\n"},
			{Role: llm.RoleAssistant, Text: "hello"},
		}}
		agent := newAgent(t, provider)
		var shown []string
		hooks := Hooks{OnAssistant: func(message llm.Message) { shown = append(shown, message.Text) }}

		for _, prompt := range []string{"one", "two"} {
			if err := agent.Run(context.Background(), prompt, hooks); err != nil {
				t.Fatal(err)
			}
		}

		if len(shown) != 2 || shown[0] != llm.SilentNote || shown[1] != "hello" || agent.History[1].Text != llm.SilentNote {
			t.Errorf("shown = %q history = %q", shown, agent.History[1].Text)
		}
	})
}

// limitedProvider refuses the first requests with err, then answers.
type limitedProvider struct {
	refusals int
	err      error
	calls    int
}

func (p *limitedProvider) Stream(context.Context, llm.Request, func(llm.Event)) (llm.Response, error) {
	p.calls++
	if p.calls <= p.refusals {
		return llm.Response{}, p.err
	}
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: "done"}}, nil
}

func limitError(code int, retryAfter, body string) error {
	header := http.Header{}
	header.Set("Retry-After", retryAfter)
	return fmt.Errorf("wrapped: %w", &llm.HTTPError{Code: code, Status: http.StatusText(code), Header: header, Body: body})
}

func TestLimitRetries(t *testing.T) {
	overloaded := `{"type":"error","error":{"type":"overloaded_error"}}`

	t.Run("AG-13 a short limit is retried after the wait asked, saying so, then the turn goes on", func(t *testing.T) {
		provider := &limitedProvider{refusals: 2, err: limitError(529, "0.01", overloaded)}
		agent := newAgent(t, provider)
		var activity []string
		hooks := Hooks{OnActivity: func(text string) { activity = append(activity, text) }}

		err := agent.Run(context.Background(), "hi", hooks)

		if err != nil || provider.calls != 3 || agent.History[len(agent.History)-1].Text != "done" {
			t.Fatalf("err = %v calls = %d", err, provider.calls)
		}
		if len(activity) != 4 || activity[0] != "overloaded · retrying in 0 s (1 of 3)" || activity[2] != "overloaded · retrying in 0 s (2 of 3)" || activity[3] != "" {
			t.Errorf("activity = %q", activity)
		}
	})

	t.Run("AG-14 a plan limit is not retried; a short one gives up after LimitRetries more tries", func(t *testing.T) {
		plan := &limitedProvider{refusals: 9, err: limitError(429, "", `{"error":{"type":"usage_limit_reached"}}`)}
		short := &limitedProvider{refusals: 9, err: limitError(529, "0.01", overloaded)}

		planErr := newAgent(t, plan).Run(context.Background(), "hi", Hooks{})
		shortErr := newAgent(t, short).Run(context.Background(), "hi", Hooks{})

		if planErr == nil || plan.calls != 1 {
			t.Errorf("plan: err = %v calls = %d", planErr, plan.calls)
		}
		if _, limited := llm.LimitOf(shortErr); !limited || short.calls != LimitRetries+1 {
			t.Errorf("short: err = %v calls = %d", shortErr, short.calls)
		}
	})
}

func TestPluginEvents(t *testing.T) {
	t.Run("AG-15 AfterToolCall changes what the model reads of a call, and what is shown", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent := newAgent(t, provider)
		agent.AfterToolCall = func(_ context.Context, call llm.ToolCall, output string, _ bool) string {
			return call.Name + " gave " + strings.ToUpper(output)
		}
		var shown string
		hooks := Hooks{OnToolDone: func(_ llm.ToolCall, output string, _ bool) { shown = output }}

		if err := agent.Run(context.Background(), "read it", hooks); err != nil {
			t.Fatal(err)
		}

		result := agent.History[2]
		if result.Role != llm.RoleTool || !strings.HasPrefix(result.Text, "read gave ") || !strings.Contains(result.Text, "HELLO") || shown != result.Text {
			t.Errorf("result = %q shown = %q", result.Text, shown)
		}
	})

	t.Run("AG-16 EndTurn hears each turn once, with its prompt, last answer and error", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "first answer"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "second answer"},
		}}
		agent := newAgent(t, provider)
		var ends []string
		agent.EndTurn = func(prompt, answer string, err error) {
			ends = append(ends, fmt.Sprintf("%s|%s|%v", prompt, answer, err))
		}

		agent.Run(context.Background(), "one", Hooks{})
		agent.Run(context.Background(), "two", Hooks{})
		provider.err = errors.New("boom")
		agent.Run(context.Background(), "three", Hooks{})

		want := []string{"one|first answer|<nil>", "two|second answer|<nil>", "three||boom"}
		if strings.Join(ends, "\n") != strings.Join(want, "\n") {
			t.Errorf("ends = %q", ends)
		}
	})
}

// pieceProvider streams its reply in pieces, then gives it.
type pieceProvider struct{}

func (pieceProvider) Stream(_ context.Context, _ llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	onEvent(llm.Event{Kind: llm.EventReasoning, Text: "hm"})
	onEvent(llm.Event{Kind: llm.EventText, Text: "hel"})
	onEvent(llm.Event{Kind: llm.EventText, Text: "lo"})
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: "hello", Reasoning: "hm"}}, nil
}

func TestStreamPieces(t *testing.T) {
	t.Run("AG-17 StreamPiece hears every piece of text and reasoning as it comes", func(t *testing.T) {
		agent := newAgent(t, pieceProvider{})
		var pieces []string
		agent.StreamPiece = func(kind llm.EventKind, text string) {
			pieces = append(pieces, fmt.Sprintf("%d:%s", kind, text))
		}

		if err := agent.Run(context.Background(), "hi", Hooks{}); err != nil {
			t.Fatal(err)
		}

		want := []string{fmt.Sprintf("%d:hm", llm.EventReasoning), fmt.Sprintf("%d:hel", llm.EventText), fmt.Sprintf("%d:lo", llm.EventText)}
		if strings.Join(pieces, ",") != strings.Join(want, ",") {
			t.Errorf("pieces = %q", pieces)
		}
	})
}
