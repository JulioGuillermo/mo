package agent

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | TM-01 | Happy    | A prompt, a reply and a tool's result each have when they happened and how long | P0 |
// | TM-02 | Happy    | A reply has when its reasoning, text and calls came, in order, with its tokens | P0  |
// | TM-03 | Happy    | A call that asks has when the user was asked and answered, and how long they took | P0 |
// | TM-04 | Happy    | A message waiting in the queue starts when sent and ends when the model read it | P1 |
// | TM-05 | Happy    | The timing is saved with the message and never sent to the model         | P0       |
// | TM-06 | Happy    | Spans are the milliseconds between the times set, 0 for those not set     | P1       |
// | TM-07 | Happy    | A compaction keeps when the summary was written, on the message it starts, the prompt's own timing kept | P1 |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
)

// pacedProvider streams each reply as reasoning, then text, then its
// calls, a pause before each, with its usage.
type pacedProvider struct {
	scriptedProvider
	pause time.Duration
}

func (p *pacedProvider) Stream(ctx context.Context, req llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	reply := p.replies[0]
	time.Sleep(p.pause)
	onEvent(llm.Event{Kind: llm.EventReasoning, Text: "thinking"})
	time.Sleep(p.pause)
	onEvent(llm.Event{Kind: llm.EventText, Text: reply.Text})
	for i, call := range reply.ToolCalls {
		time.Sleep(p.pause)
		onEvent(llm.Event{Kind: llm.EventToolCall, Call: i, Name: call.Name, ID: call.ID, Text: string(call.Args)})
	}
	time.Sleep(p.pause)
	response, err := p.scriptedProvider.Stream(ctx, req, onEvent)
	response.Usage = llm.Usage{InputTokens: 120, OutputTokens: 30, GenerationRate: 42.5}
	return response, err
}

func TestTiming(t *testing.T) {
	pause := 15 * time.Millisecond

	t.Run("TM-01 a prompt, a reply and a tool's result each have when they happened and how long", func(t *testing.T) {
		provider := &pacedProvider{pause: pause, scriptedProvider: scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "reading", ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}}
		agent := newAgent(t, provider)
		before := time.Now()

		if err := agent.Run(context.Background(), "read a.txt", Hooks{}); err != nil {
			t.Fatal(err)
		}

		prompt, reply, result, answer := agent.History[0], agent.History[1], agent.History[2], agent.History[3]
		for _, message := range []llm.Message{prompt, reply, result, answer} {
			if message.Timing == nil || message.Timing.Start.Before(before) {
				t.Fatalf("%s has no timing: %+v", message.Role, message.Timing)
			}
		}
		if !reply.Timing.Start.Before(result.Timing.Start) || result.Timing.End.After(answer.Timing.Start) {
			t.Errorf("out of order: reply %v result %v..%v answer %v", reply.Timing.Start, result.Timing.Start, result.Timing.End, answer.Timing.Start)
		}
		if reply.Timing.TotalMS < 3*pause.Milliseconds() || reply.Timing.End.IsZero() {
			t.Errorf("reply took %d ms", reply.Timing.TotalMS)
		}
	})

	t.Run("TM-02 a reply has when its reasoning, text and calls came, in order, with its tokens", func(t *testing.T) {
		provider := &pacedProvider{pause: pause, scriptedProvider: scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "reading", ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}}
		agent := newAgent(t, provider)

		_ = agent.Run(context.Background(), "read a.txt", Hooks{})

		timing := agent.History[1].Timing
		order := []time.Time{timing.Start, timing.FirstToken, timing.TextStart, timing.CallsStart, timing.End}
		for i := 1; i < len(order); i++ {
			if order[i].Before(order[i-1]) {
				t.Errorf("time %d before the one before it: %v", i, order)
			}
		}
		if !timing.FirstToken.Equal(timing.ReasoningStart) || timing.FirstTokenMS < pause.Milliseconds() || timing.CallsMS <= 0 {
			t.Errorf("first token %v reasoning %v first token ms %d calls ms %d", timing.FirstToken, timing.ReasoningStart, timing.FirstTokenMS, timing.CallsMS)
		}
		if timing.Usage == nil || timing.Usage.OutputTokens != 30 || timing.Usage.GenerationRate != 42.5 || timing.Retries != 0 {
			t.Errorf("usage %+v retries %d", timing.Usage, timing.Retries)
		}
	})

	t.Run("TM-03 a call that asks has when the user was asked and answered, and how long they took", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "write", `{"path":"b.txt","content":"x"}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider, "write")
		hooks := Hooks{Interact: func(context.Context, Interaction) Reply {
			time.Sleep(pause)
			return Reply{Decisions: []Decision{Allow}}
		}}

		_ = agent.Run(context.Background(), "write", hooks)

		timing := agent.History[2].Timing
		if timing.Asked.IsZero() || timing.WaitedMS < pause.Milliseconds() || timing.Start.Before(timing.Answered) {
			t.Errorf("asked %v answered %v waited %d start %v", timing.Asked, timing.Answered, timing.WaitedMS, timing.Start)
		}
	})

	t.Run("TM-04 a message waiting in the queue starts when sent and ends when the model read it", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}}}
		agent := newAgent(t, provider)
		sent := time.Now().Add(-2 * time.Second)
		agent.QueueTyped(Queued{Text: "and this", Typed: "and this", At: sent})

		agent.deliverQueued(Hooks{})

		timing := agent.History[len(agent.History)-1].Timing
		if timing == nil || !timing.Start.Equal(sent) || timing.TotalMS < 2000 {
			t.Errorf("timing %+v", timing)
		}
	})

	t.Run("TM-05 the timing is saved with the message and never sent to the model", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "one"}, {Role: llm.RoleAssistant, Text: "two"}}}
		agent := newAgent(t, provider)
		_ = agent.Run(context.Background(), "first", Hooks{})
		_ = agent.Run(context.Background(), "second", Hooks{})

		saved, _ := json.Marshal(agent.History[0])
		sent, _ := json.Marshal(provider.requests[1])

		if !strings.Contains(string(saved), `"Timing":{"start":`) {
			t.Errorf("saved %s", saved)
		}
		if strings.Contains(string(sent), `"start"`) || strings.Contains(string(sent), "total_ms") {
			t.Errorf("sent the timing: %s", sent)
		}
	})

	t.Run("TM-06 spans are the milliseconds between the times set, 0 for those not set", func(t *testing.T) {
		start := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
		timing := (&llm.Timing{Start: start, FirstToken: start.Add(1500 * time.Millisecond), TextStart: start.Add(2 * time.Second),
			TextEnd: start.Add(5 * time.Second), End: start.Add(6 * time.Second)}).Ended()

		if timing.TotalMS != 6000 || timing.FirstTokenMS != 1500 || timing.TextMS != 3000 || timing.ReasoningMS != 0 || timing.WaitedMS != 0 {
			t.Errorf("%+v", timing)
		}
	})

	t.Run("TM-07 a compaction keeps when the summary was written, on the message it starts, the prompt's own timing kept", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "- build fixed in a.txt"},
			{Role: llm.RoleAssistant, Text: "tests pass"},
		}}
		agent := newAgent(t, provider)
		agent.Window, agent.CompactAt = 1000, 0.75
		agent.History = longHistory()
		agent.LastUsage = llm.Usage{InputTokens: 800}
		agent.historyAtUsage = len(agent.History)

		if err := agent.Run(context.Background(), "now run the tests", Hooks{}); err != nil {
			t.Fatal(err)
		}

		first := agent.History[0].Timing
		if first == nil || first.Summary == nil || first.Summary.End.IsZero() || first.Start.IsZero() {
			t.Errorf("first message timing %+v", first)
		}
	})
}
