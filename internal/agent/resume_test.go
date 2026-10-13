package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | RE-01 | Happy    | An answer stopped mid-way stays in the history; continue tells the model to go on | P0 |
// | RE-02 | Happy    | Stopped before any answer, continue answers the prompt waiting, with no note | P1  |
// | RE-03 | Edge     | Continue on an empty conversation says there is nothing to continue  | P2       |
// | RE-04 | Happy    | A stop amid tool calls keeps those that ran and the text, drops the rest | P0    |

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
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
	t.Run("RE-01 an answer stopped mid-way stays in the history; continue tells the model to go on", func(t *testing.T) {
		ctx, stop := context.WithCancel(context.Background())
		provider := &stoppingProvider{stop: stop}
		agent := newAgent(t, provider)

		stopped := agent.Run(ctx, "do both steps", Hooks{})
		kept := agent.History[len(agent.History)-1]
		err := agent.Continue(context.Background(), Hooks{})

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
