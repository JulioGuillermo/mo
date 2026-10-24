package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | RE-01 | Happy    | An answer stopped mid-way stays in the history; continue keep tells the model to go on | P0 |
// | RE-02 | Happy    | Stopped before any answer, continue answers the prompt waiting, with no note | P1  |
// | RE-03 | Edge     | Continue on an empty conversation says there is nothing to continue  | P2       |
// | RE-04 | Happy    | A stop amid tool calls keeps those that ran and the text, drops the rest | P0    |
// | RE-05 | Happy    | A resumable call a stop cut stays unanswered; continue runs it as resumed and goes on | P0 |
// | RE-06 | Security | A call left without a result that is not resumable is answered as interrupted, never run again | P0 |
// | RE-07 | Happy    | A new prompt instead of continue answers the call left as stopped        | P0       |
// | RE-08 | Happy    | Each step gives a copy of the history                                    | P1       |
// | RE-09 | Happy    | Continue drops an answer stopped mid-way and asks the model again, with no note | P0 |

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// stoppingProvider writes the start of an answer, then the user stops it;
// afterwards it answers whole.
type stoppingProvider struct {
	stop     context.CancelFunc
	requests []llm.Request
}

func (p *stoppingProvider) Stream(ctx context.Context, req llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	p.requests = append(p.requests, req)
	if p.stop != nil {
		onEvent(llm.Event{Kind: llm.EventText, Text: "Step one is done, now step"})
		p.stop()
		p.stop = nil
		return llm.Response{}, ctx.Err()
	}
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: "two is done too"}}, nil
}

func TestContinue(t *testing.T) {
	t.Run("RE-01 an answer stopped mid-way stays in the history; continue keep tells the model to go on", func(t *testing.T) {
		ctx, stop := context.WithCancel(context.Background())
		provider := &stoppingProvider{stop: stop}
		agent := newAgent(t, provider)

		stopped := agent.Run(ctx, "do both steps", Hooks{})
		kept := agent.History[len(agent.History)-1]
		err := agent.ContinueTelling(context.Background(), Hooks{})

		sent := provider.requests[1].Messages
		if !errors.Is(stopped, context.Canceled) || kept.Role != llm.RoleAssistant || !strings.HasPrefix(kept.Text, "Step one is done, now step") {
			t.Fatalf("stopped = %v kept = %+v", stopped, kept)
		}
		if err != nil || sent[len(sent)-1].Text != continueNote || agent.History[len(agent.History)-1].Text != "two is done too" {
			t.Errorf("err = %v last sent = %q", err, sent[len(sent)-1].Text)
		}
	})

	t.Run("RE-02 stopped before any answer, continue answers the prompt waiting, with no note", func(t *testing.T) {
		provider := &stoppingProvider{}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "what is 2+2"}}

		err := agent.Continue(context.Background(), Hooks{})

		sent := provider.requests[0].Messages
		if err != nil || len(sent) != 1 || sent[0].Text != "what is 2+2" {
			t.Errorf("err = %v sent = %+v", err, sent)
		}
	})

	t.Run("RE-09 continue drops an answer stopped mid-way and asks the model again, with no note", func(t *testing.T) {
		ctx, stop := context.WithCancel(context.Background())
		provider := &stoppingProvider{stop: stop}
		agent := newAgent(t, provider)
		_ = agent.Run(ctx, "do both steps", Hooks{})

		err := agent.Continue(context.Background(), Hooks{})

		sent := provider.requests[1].Messages
		if err != nil || len(sent) != 1 || sent[0].Text != "do both steps" {
			t.Fatalf("err = %v sent = %+v", err, sent)
		}
		if len(agent.History) != 2 || agent.History[1].Text != "two is done too" {
			t.Errorf("history = %+v", agent.History)
		}
	})

	t.Run("RE-03 continue on an empty conversation says there is nothing to continue", func(t *testing.T) {
		if err := newAgent(t, &stoppingProvider{}).Continue(context.Background(), Hooks{}); !errors.Is(err, ErrNothingToContinue) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestStopInToolCalls(t *testing.T) {
	t.Run("RE-04 a stop amid tool calls keeps those that ran and the text, drops the rest", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "Checking both.", ToolCalls: []llm.ToolCall{
			toolCall("ran", "read", `{"path":"a.txt"}`),
			toolCall("cut", "read", `{"path":"a.txt","offset":2}`),
		}}}}
		agent := newAgent(t, provider)
		ctx, cancel := context.WithCancel(context.Background())
		hooks := Hooks{OnToolDone: func(call llm.ToolCall, _ string, _ bool) {
			if call.ID == "" || strings.Contains(string(call.Args), "offset") {
				return
			}
			cancel()
		}}

		_ = agent.Run(ctx, "go", hooks)

		reply := agent.History[1]
		if len(agent.History) != 3 || reply.Text != "Checking both." || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].ID != "ran" || agent.History[2].ToolCallID != "ran" {
			t.Errorf("history = %+v", agent.History)
		}
	})
}

func resumableAgent(t *testing.T, provider llm.Provider, runs *[]bool, started chan struct{}) *Agent {
	t.Helper()
	job := tools.Tool{Name: "long", Resumable: true, Schema: []byte(`{"type":"object","properties":{}}`),
		Run: func(ctx context.Context, _ tools.Env, _ json.RawMessage) (string, error) {
			*runs = append(*runs, tools.Resuming(ctx))
			if tools.Resuming(ctx) {
				return "went on and ended", nil
			}
			close(started)
			<-ctx.Done()
			return "stopped", nil
		}}
	agent := newAgent(t, provider)
	agent.Tools = tools.NewRegistry(tools.Env{WorkDir: t.TempDir()}, tools.Options{Extra: []tools.Tool{job}})
	return agent
}

func TestResumable(t *testing.T) {
	t.Run("RE-05 a resumable call a stop cut stays, unanswered; continue runs it as resumed and the turn goes on", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "long", `{}`)}},
			{Role: llm.RoleAssistant, Text: "all done"},
		}}
		var runs []bool
		started := make(chan struct{})
		agent := resumableAgent(t, provider, &runs, started)
		ctx, stop := context.WithCancel(context.Background())
		go func() {
			<-started
			stop()
		}()

		stopped := agent.Run(ctx, "go", Hooks{})
		unanswered := agent.unanswered()
		err := agent.Continue(context.Background(), Hooks{})

		if !errors.Is(stopped, context.Canceled) || len(unanswered) != 1 || unanswered[0].ID != "c1" {
			t.Fatalf("stopped %v unanswered %+v", stopped, unanswered)
		}
		last := agent.History[len(agent.History)-1]
		if err != nil || len(runs) != 2 || runs[0] || !runs[1] || last.Text != "all done" || strings.Contains(provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Text, "stopped you") {
			t.Errorf("err %v runs %v history %+v", err, runs, agent.History)
		}
	})

	t.Run("RE-06 a call left without a result that is not resumable is answered as interrupted, never run again", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "noted"}}}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "go"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("b1", "bash", `{"command":"rm -rf x"}`)}}}

		err := agent.Continue(context.Background(), Hooks{})

		if err != nil || agent.History[2].ToolCallID != "b1" || agent.History[2].Text != interruptedCall {
			t.Errorf("err %v history %+v", err, agent.History)
		}
	})

	t.Run("RE-07 a new prompt instead of continue answers the call left as stopped", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}}}
		agent := newAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "go"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("a1", "agent", `{"task":"x"}`)}}}

		err := agent.Run(context.Background(), "something else", Hooks{})

		if err != nil || agent.History[2].ToolCallID != "a1" || agent.History[2].Text != stoppedCall || agent.History[3].Text != "something else" {
			t.Errorf("err %v history %+v", err, agent.History)
		}
	})

	t.Run("RE-08 each step gives a copy of the history", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("r1", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "read it"},
		}}
		agent := newAgent(t, provider)
		var steps []int

		_ = agent.Run(context.Background(), "read a.txt", Hooks{OnStep: func(history []llm.Message) { steps = append(steps, len(history)) }})

		if len(steps) != 3 || steps[0] != 2 || steps[1] != 3 || steps[2] != 4 {
			t.Errorf("steps %v", steps)
		}
	})
}
