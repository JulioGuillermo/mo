package agent

// Test Traceability Table
// | ID    | Category | Description                                                                         | Priority |
// | ST-01 | Happy    | A stop that lets the calls end cuts the reply being written, kept marked as stopped  | P0       |
// | ST-02 | Happy    | A stop while a call runs lets it end with its result, runs none of the others         | P0       |
// | ST-03 | Happy    | Stop next lets the round end, all its calls, and does not ask the model again         | P0       |
// | ST-04 | Happy    | A stop taken back lets the run go on                                                  | P1       |
// | ST-05 | Happy    | A stop amid a reply that runs a call cuts the reply, lets the call end and keeps it    | P0       |
// | ST-06 | Happy    | Continue drops the cut text, keeps the call that ran and tells the model nothing       | P0       |
// | ST-07 | Happy    | A resumable call a stop cut short stays without a result; continue resumes it          | P0       |
// | ST-08 | Happy    | A stop that holds calls back still shows the model the images the calls that ran read  | P1       |

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// slowTool runs a call named by its "name" argument: during reaches it as it
// runs, and the names run are kept in order.
type slowTool struct {
	mu     sync.Mutex
	ran    []string
	during func(name string)
}

func (s *slowTool) tool() tools.Tool {
	return tools.Tool{Name: "slow", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
		Run: func(_ context.Context, _ tools.Env, raw json.RawMessage) (string, error) {
			var args struct{ Name string }
			_ = json.Unmarshal(raw, &args)
			s.mu.Lock()
			s.ran = append(s.ran, args.Name)
			s.mu.Unlock()
			if s.during != nil {
				s.during(args.Name)
			}
			return args.Name + " done", nil
		}}
}

func (s *slowTool) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

func slowCall(id, name string) llm.ToolCall {
	return toolCall(id, "slow", `{"name":"`+name+`"}`)
}

func slowAgent(t *testing.T, provider llm.Provider, slow *slowTool) *Agent {
	t.Helper()
	agent, _ := slowAgentIn(t, provider, slow)
	return agent
}

func slowAgentIn(t *testing.T, provider llm.Provider, slow *slowTool) (*Agent, string) {
	t.Helper()
	agent := newAgent(t, provider)
	workDir := t.TempDir()
	agent.Tools = tools.NewRegistry(tools.Env{WorkDir: workDir}, tools.Options{Extra: []tools.Tool{slow.tool()}})
	return agent, workDir
}

// cutProvider writes the start of a reply, has the test stop it, and ends
// when the stop cuts it; afterwards it answers as replies say.
type cutProvider struct {
	mu       sync.Mutex
	stop     func()
	calls    []llm.ToolCall
	replies  []llm.Message
	requests []llm.Request
}

func (p *cutProvider) Stream(ctx context.Context, req llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	stop := p.stop
	p.stop = nil
	p.mu.Unlock()
	if stop == nil {
		reply := p.replies[0]
		p.replies = p.replies[1:]
		return llm.Response{Message: reply}, nil
	}
	onEvent(llm.Event{Kind: llm.EventText, Text: "Let me check, then"})
	for i, call := range p.calls {
		onEvent(llm.Event{Kind: llm.EventToolCall, Call: i, ID: call.ID, Name: call.Name, Text: string(call.Args)})
	}
	// A piece of the next call sends the last one whole to run.
	onEvent(llm.Event{Kind: llm.EventToolCall, Call: len(p.calls), ID: "unwritten", Name: "slow"})
	stop()
	<-ctx.Done()
	return llm.Response{}, ctx.Err()
}

func TestSoftStop(t *testing.T) {
	ctx := context.Background()

	t.Run("ST-01 a stop that lets the calls end cuts the reply being written, kept marked as stopped", func(t *testing.T) {
		provider := &cutProvider{}
		agent := newAgent(t, provider)
		provider.stop = func() { agent.RequestStop(StopAfterCalls) }

		err := agent.Run(ctx, "check", Hooks{})

		kept := agent.History[len(agent.History)-1]
		if !errors.Is(err, context.Canceled) || !kept.Stopped || kept.Text != "Let me check, then"+stoppedMark || len(provider.requests) != 1 {
			t.Errorf("err = %v kept = %+v requests = %d", err, kept, len(provider.requests))
		}
	})

	t.Run("ST-02 a stop while a call runs lets it end with its result, runs none of the others", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{slowCall("1", "a"), slowCall("2", "b")}}}}
		slow := &slowTool{}
		agent := slowAgent(t, provider, slow)
		slow.during = func(string) { agent.RequestStop(StopAfterCalls) }

		err := agent.Run(ctx, "go", Hooks{})

		reply, result := agent.History[1], agent.History[2]
		if !errors.Is(err, context.Canceled) || strings.Join(slow.names(), ",") != "a" || len(provider.requests) != 1 {
			t.Fatalf("err = %v ran = %v requests = %d", err, slow.names(), len(provider.requests))
		}
		if len(agent.History) != 3 || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].ID != "1" || result.Text != "a done" {
			t.Errorf("history = %+v", agent.History)
		}
	})

	t.Run("ST-03 stop next lets the round end, all its calls, and does not ask the model again", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{slowCall("1", "a"), slowCall("2", "b")}}}}
		slow := &slowTool{}
		agent := slowAgent(t, provider, slow)
		slow.during = func(string) { agent.RequestStop(StopAfterRound) }

		err := agent.Run(ctx, "go", Hooks{})

		if !errors.Is(err, context.Canceled) || strings.Join(slow.names(), ",") != "a,b" || len(provider.requests) != 1 || len(agent.History) != 4 {
			t.Errorf("err = %v ran = %v requests = %d history = %+v", err, slow.names(), len(provider.requests), agent.History)
		}
	})

	t.Run("ST-04 a stop taken back lets the run go on", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{slowCall("1", "a"), slowCall("2", "b")}},
			{Role: llm.RoleAssistant, Text: "both done"},
		}}
		slow := &slowTool{}
		agent := slowAgent(t, provider, slow)
		slow.during = func(string) {
			agent.RequestStop(StopAfterCalls)
			agent.ClearStop()
		}

		err := agent.Run(ctx, "go", Hooks{})

		if err != nil || strings.Join(slow.names(), ",") != "a,b" || agent.History[len(agent.History)-1].Text != "both done" {
			t.Errorf("err = %v ran = %v history = %+v", err, slow.names(), agent.History)
		}
	})

	cutAmidCall := func(t *testing.T) (*Agent, *cutProvider, *slowTool) {
		t.Helper()
		provider := &cutProvider{calls: []llm.ToolCall{slowCall("1", "a")}, replies: []llm.Message{{Role: llm.RoleAssistant, Text: "a checked"}}}
		slow := &slowTool{}
		agent := slowAgent(t, provider, slow)
		agent.EarlyCalls = true
		started, release := make(chan struct{}), make(chan struct{})
		slow.during = func(string) {
			close(started)
			<-release
		}
		provider.stop = func() {
			<-started
			agent.RequestStop(StopAfterCalls)
			close(release)
		}
		allow := func(context.Context, Interaction) Reply { return Reply{} }
		if err := agent.Run(ctx, "check", Hooks{Interact: allow}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		return agent, provider, slow
	}

	t.Run("ST-05 a stop amid a reply that runs a call cuts the reply, lets the call end and keeps it", func(t *testing.T) {
		agent, _, slow := cutAmidCall(t)

		reply, result := agent.History[1], agent.History[2]
		if strings.Join(slow.names(), ",") != "a" || len(agent.History) != 3 {
			t.Fatalf("ran = %v history = %+v", slow.names(), agent.History)
		}
		if !reply.Stopped || !strings.HasSuffix(reply.Text, stoppedMark) || len(reply.ToolCalls) != 1 || result.ToolCallID != "1" || result.Text != "a done" {
			t.Errorf("reply = %+v result = %+v", reply, result)
		}
	})

	t.Run("ST-06 continue drops the cut text, keeps the call that ran and tells the model nothing", func(t *testing.T) {
		agent, provider, _ := cutAmidCall(t)
		agent.ClearStop()

		err := agent.Continue(ctx, Hooks{})

		sent := provider.requests[1].Messages
		if err != nil || len(sent) != 3 || sent[1].Text != "" || sent[1].Stopped || len(sent[1].ToolCalls) != 1 || sent[2].Text != "a done" {
			t.Errorf("err = %v sent = %+v", err, sent)
		}
		if last := agent.History[len(agent.History)-1]; last.Text != "a checked" {
			t.Errorf("history = %+v", agent.History)
		}
	})

	t.Run("ST-07 a resumable call a stop cut short stays without a result; continue resumes it", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "team", `{}`)}},
			{Role: llm.RoleAssistant, Text: "all done"},
		}}
		var agent *Agent
		var resumed []bool
		team := tools.Tool{Name: "team", Resumable: true, Schema: []byte(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ tools.Env, _ json.RawMessage) (string, error) {
				resumed = append(resumed, tools.Resuming(ctx))
				if tools.Resuming(ctx) {
					return "they ended", nil
				}
				agent.RequestStop(StopAfterRound)
				tools.MarkCutShort(ctx)
				return "they stopped", nil
			}}
		agent = newAgent(t, provider)
		agent.Tools = tools.NewRegistry(tools.Env{WorkDir: t.TempDir()}, tools.Options{Extra: []tools.Tool{team}})

		stopped := agent.Run(ctx, "go", Hooks{})
		unanswered := agent.unanswered()
		agent.ClearStop()
		err := agent.Continue(ctx, Hooks{})

		if !errors.Is(stopped, context.Canceled) || len(unanswered) != 1 || unanswered[0].ID != "c1" {
			t.Fatalf("stopped = %v unanswered = %+v", stopped, unanswered)
		}
		if err != nil || len(resumed) != 2 || !resumed[1] || agent.History[len(agent.History)-1].Text != "all done" {
			t.Errorf("err = %v resumed = %v history = %+v", err, resumed, agent.History)
		}
	})

	t.Run("ST-08 a stop that holds calls back still shows the model the images the calls that ran read", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			toolCall("1", "read", `{"path":"shot.png"}`), slowCall("2", "a"), slowCall("3", "b"),
		}}}}
		slow := &slowTool{}
		agent, workDir := slowAgentIn(t, provider, slow)
		slow.during = func(string) { agent.RequestStop(StopAfterCalls) }
		picture, err := os.Create(filepath.Join(workDir, "shot.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(picture, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
			t.Fatal(err)
		}
		picture.Close()

		stopped := agent.Run(ctx, "look", Hooks{})

		note := agent.History[len(agent.History)-1]
		if !errors.Is(stopped, context.Canceled) || strings.Join(slow.names(), ",") != "a" {
			t.Fatalf("stopped = %v ran = %v", stopped, slow.names())
		}
		if note.Role != llm.RoleUser || len(note.Images) != 1 || !strings.Contains(note.Text, "shot.png") {
			t.Errorf("last = %+v", note)
		}
	})
}
