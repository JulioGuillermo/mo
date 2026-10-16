package agent

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | EC-01 | Happy    | A call runs as soon as it is written, before the reply ends; results go in order | P0  |
// | EC-02 | Security | A call that asks is asked while the reply goes on; the next waits for its answer | P0  |
// | EC-03 | Edge     | A reply that breaks off keeps the calls it ran, with their results, and is not sent again | P0 |
// | EC-04 | Happy    | What the reply wrote before its first call shows once, before the call runs | P1      |
// | EC-05 | Edge     | Off, or with nobody to ask, the calls run once the reply ends               | P1       |
// | EC-06 | Edge     | The loop guard sees the calls the reply already ran, as when they run once it ends | P1 |

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
)

// piecedProvider streams each reply's text and then its calls in pieces,
// waiting at each step for check, with a timeout, so a test can see what
// ran while the reply was still being written.
type piecedProvider struct {
	mu       sync.Mutex
	replies  []llm.Message
	requests int
	// whileWriting runs after a reply's last call has started streaming,
	// before the reply ends; failAfter breaks the reply there instead.
	whileWriting func()
	failAfter    error
}

func (p *piecedProvider) Stream(_ context.Context, _ llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	p.mu.Lock()
	p.requests++
	reply := p.replies[0]
	p.replies = p.replies[1:]
	p.mu.Unlock()
	if reply.Text != "" {
		onEvent(llm.Event{Kind: llm.EventText, Text: reply.Text})
	}
	for i, call := range reply.ToolCalls {
		args := string(call.Args)
		onEvent(llm.Event{Kind: llm.EventToolCall, Call: i, ID: call.ID, Name: call.Name})
		onEvent(llm.Event{Kind: llm.EventToolCall, Call: i, Text: args[:len(args)/2]})
		onEvent(llm.Event{Kind: llm.EventToolCall, Call: i, Text: args[len(args)/2:]})
	}
	if len(reply.ToolCalls) > 0 && p.whileWriting != nil {
		p.whileWriting()
	}
	if len(reply.ToolCalls) > 0 && p.failAfter != nil {
		return llm.Response{}, p.failAfter
	}
	return llm.Response{Message: reply}, nil
}

// waitFor waits a little for a condition that another goroutine makes true.
func waitFor(condition func() bool) bool {
	for range 200 {
		if condition() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func earlyAgent(t *testing.T, provider llm.Provider, confirm ...string) (*Agent, string) {
	t.Helper()
	agent, workDir := newAgentIn(t, provider, confirm...)
	agent.EarlyCalls = true
	return agent, workDir
}

func writeCall(id, path, content string) llm.ToolCall {
	return toolCall(id, "write", `{"path":"`+path+`","content":"`+content+`"}`)
}

func TestEarlyCalls(t *testing.T) {
	ctx := context.Background()
	answerAll := func(decision Decision) func(context.Context, Interaction) Reply {
		return func(_ context.Context, interaction Interaction) Reply {
			reply := Reply{Decisions: make([]Decision, len(interaction.Approvals))}
			for i := range reply.Decisions {
				reply.Decisions[i] = decision
			}
			return reply
		}
	}

	t.Run("EC-01 a call runs as soon as it is written, before the reply ends; results go in order", func(t *testing.T) {
		provider := &piecedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{writeCall("1", "first.txt", "one"), writeCall("2", "second.txt", "two")}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent, workDir := earlyAgent(t, provider)
		ranEarly := false
		provider.whileWriting = func() {
			ranEarly = waitFor(func() bool { _, err := os.Stat(filepath.Join(workDir, "first.txt")); return err == nil })
		}

		err := agent.Run(ctx, "write", Hooks{Interact: answerAll(Allow)})

		if err != nil || !ranEarly {
			t.Fatalf("err = %v, the first call ran before the reply ended: %v", err, ranEarly)
		}
		results := agent.History[2:4]
		if results[0].ToolCallID != "1" || results[1].ToolCallID != "2" || results[0].IsError || results[1].IsError {
			t.Errorf("results = %+v", results)
		}
		if _, err := os.Stat(filepath.Join(workDir, "second.txt")); err != nil {
			t.Error("the second call should run once the reply ends")
		}
	})

	t.Run("EC-02 a call that asks is asked while the reply goes on; the next waits for its answer", func(t *testing.T) {
		provider := &piecedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{writeCall("1", "denied.txt", "no"), writeCall("2", "allowed.txt", "yes")}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent, workDir := earlyAgent(t, provider, "write")
		var mu sync.Mutex
		var asked []string
		release := make(chan struct{})
		askedWhileWriting := false
		provider.whileWriting = func() {
			askedWhileWriting = waitFor(func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) == 1 })
			close(release)
		}
		interact := func(_ context.Context, interaction Interaction) Reply {
			mu.Lock()
			asked = append(asked, interaction.Approvals[0].Call.ID)
			first := len(asked) == 1
			mu.Unlock()
			if first {
				<-release
				_, secondRan := os.Stat(filepath.Join(workDir, "allowed.txt"))
				if secondRan == nil {
					t.Error("the second call ran before the first was answered")
				}
				return Reply{Decisions: []Decision{Deny}}
			}
			return Reply{Decisions: []Decision{Allow}}
		}

		err := agent.Run(ctx, "write", Hooks{Interact: interact})

		_, denied := os.Stat(filepath.Join(workDir, "denied.txt"))
		_, allowed := os.Stat(filepath.Join(workDir, "allowed.txt"))
		if err != nil || !askedWhileWriting || len(asked) != 2 || denied == nil || allowed != nil {
			t.Fatalf("err = %v asked while writing %v asked = %v denied ran %v allowed ran %v", err, askedWhileWriting, asked, denied == nil, allowed == nil)
		}
		if agent.History[2].Text != deniedResult || agent.History[3].IsError {
			t.Errorf("results = %+v", agent.History[2:4])
		}
	})

	t.Run("EC-03 a reply that breaks off keeps the calls it ran, with their results, and is not sent again", func(t *testing.T) {
		provider := &piecedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "writing", ToolCalls: []llm.ToolCall{writeCall("1", "kept.txt", "one"), writeCall("2", "cut.txt", "two")}},
		}, failAfter: errors.New("connection reset")}
		agent, workDir := earlyAgent(t, provider)
		provider.whileWriting = func() {
			waitFor(func() bool { _, err := os.Stat(filepath.Join(workDir, "kept.txt")); return err == nil })
		}

		err := agent.Run(ctx, "write", Hooks{Interact: answerAll(Allow)})

		if err == nil || provider.requests != 1 {
			t.Fatalf("err = %v requests = %d", err, provider.requests)
		}
		reply, result := agent.History[1], agent.History[2]
		if reply.Role != llm.RoleAssistant || reply.Text != "writing" || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].ID != "1" ||
			result.ToolCallID != "1" || len(agent.History) != 3 {
			t.Errorf("history = %+v", agent.History)
		}
		if _, err := os.Stat(filepath.Join(workDir, "cut.txt")); err == nil {
			t.Error("the call the reply did not finish writing should not run")
		}
	})

	t.Run("EC-04 what the reply wrote before its first call shows once, before the call runs", func(t *testing.T) {
		provider := &piecedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "Let me write it.", ToolCalls: []llm.ToolCall{writeCall("1", "a.txt", "x"), writeCall("2", "b.txt", "y")}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent, _ := earlyAgent(t, provider)
		var mu sync.Mutex
		var shown []string
		hooks := Hooks{Interact: answerAll(Allow),
			OnAssistant: func(message llm.Message) { mu.Lock(); shown = append(shown, "text:"+message.Text); mu.Unlock() },
			OnToolStart: func(call llm.ToolCall, _ string) { mu.Lock(); shown = append(shown, "run:"+call.ID); mu.Unlock() },
		}

		_ = agent.Run(ctx, "write", hooks)

		got := strings.Join(shown, " | ")
		if !strings.HasPrefix(got, "text:Let me write it. | ") || strings.Count(got, "Let me write it.") != 1 ||
			strings.Index(got, "run:1") > strings.Index(got, "run:2") || !strings.HasSuffix(got, "text:done") {
			t.Errorf("shown = %q", got)
		}
		if agent.History[1].Text != "Let me write it." {
			t.Errorf("the reply in the history should keep its whole text: %q", agent.History[1].Text)
		}
	})

	t.Run("EC-05 off, or with nobody to ask, the calls run once the reply ends", func(t *testing.T) {
		for name, setup := range map[string]func(*Agent) Hooks{
			"off":         func(a *Agent) Hooks { a.EarlyCalls = false; return Hooks{Interact: answerAll(Allow)} },
			"nobody asks": func(*Agent) Hooks { return Hooks{} },
		} {
			provider := &piecedProvider{replies: []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{writeCall("1", "late.txt", "x")}},
				{Role: llm.RoleAssistant, Text: "done"},
			}}
			agent, workDir := earlyAgent(t, provider)
			early := true
			provider.whileWriting = func() {
				time.Sleep(50 * time.Millisecond)
				_, err := os.Stat(filepath.Join(workDir, "late.txt"))
				early = err == nil
			}
			hooks := setup(agent)

			_ = agent.Run(ctx, "write", hooks)

			if _, err := os.Stat(filepath.Join(workDir, "late.txt")); early || err != nil {
				t.Errorf("%s: ran early %v, ran at all %v", name, early, err == nil)
			}
		}
	})

	t.Run("EC-06 the loop guard sees the calls the reply already ran, as when they run once it ends", func(t *testing.T) {
		for _, early := range []bool{true, false} {
			read := toolCall("", "read", `{"path":"a.txt"}`)
			first, second, third := read, read, read
			first.ID, second.ID, third.ID = "1", "2", "3"
			provider := &piecedProvider{replies: []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{first, second, third}},
				{Role: llm.RoleAssistant, Text: "done"},
			}}
			agent, _ := earlyAgent(t, provider)
			agent.EarlyCalls = early
			agent.Loop = LoopGuard{Enabled: true, Replies: 4, Repeats: 2, StreamRepeats: 100, Warnings: 5}

			_ = agent.Run(ctx, "read it", Hooks{Interact: answerAll(Allow)})

			results := agent.History[2:5]
			if results[0].IsError || !strings.HasPrefix(results[1].Text, loopNotePrefix) || !strings.HasPrefix(results[2].Text, loopNotePrefix) {
				t.Errorf("early %v: results = %+v", early, results)
			}
		}
	})
}
