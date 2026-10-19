package agent

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PA-01 | Happy    | Sending with outgoing tools follows the mode: ask and edits ask, all not  | P0       |
// | PA-02 | Happy    | Outgoing ask always asks, allow never, also in mode all                   | P0       |
// | PA-03 | Security | Outgoing auto lets through what the model judges safe and asks otherwise  | P0       |
// | PA-04 | Happy    | A list of peers never asks                                               | P1       |
// | PA-05 | Happy    | A message let in reaches the model once, marked as not the user's        | P0       |
// | PA-06 | Happy    | Incoming tools is worked out from the session's mode                     | P1       |

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/peers"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

func peerAgent(t *testing.T, mode permissions.Mode, outgoing string, provider llm.Provider) (*Agent, *peers.Node) {
	t.Helper()
	node := peers.NewNode(t.TempDir())
	node.SetOutgoing(outgoing)
	policy, err := permissions.New(mode, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{
		Provider:    provider,
		Tools:       tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Peers: node}, tools.Options{}),
		Permissions: policy,
	}
	return agent, node
}

func peerCall(action string) llm.ToolCall {
	return toolCall("p", tools.PeersName, `{"action":"`+action+`","to":"api@dev","text":"is the token check done?"}`)
}

func TestPeerPermissions(t *testing.T) {
	ctx := context.Background()

	t.Run("PA-01 sending with outgoing tools follows the mode: ask and edits ask, all not", func(t *testing.T) {
		asks := map[permissions.Mode]bool{}
		for _, mode := range []permissions.Mode{permissions.ModeAsk, permissions.ModeEdits, permissions.ModeAll} {
			agent, _ := peerAgent(t, mode, "tools", nil)
			asks[mode] = agent.approvalFor(ctx, peerCall("send"), Hooks{}).ask
		}

		if !asks[permissions.ModeAsk] || !asks[permissions.ModeEdits] || asks[permissions.ModeAll] {
			t.Errorf("asks = %v", asks)
		}
	})

	t.Run("PA-02 outgoing ask always asks, allow never, also in mode all", func(t *testing.T) {
		asking, _ := peerAgent(t, permissions.ModeAll, "ask", nil)
		allowing, _ := peerAgent(t, permissions.ModeAsk, "allow", nil)

		if !asking.approvalFor(ctx, peerCall("send"), Hooks{}).ask || allowing.approvalFor(ctx, peerCall("send"), Hooks{}).ask {
			t.Error("ask should ask and allow should not")
		}
	})

	t.Run("PA-03 outgoing auto lets through what the model judges safe and asks otherwise", func(t *testing.T) {
		judge := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "SAFE a question about code"},
			{Role: llm.RoleAssistant, Text: "UNSAFE holds a token"},
		}}
		agent, _ := peerAgent(t, permissions.ModeAsk, "auto", judge)

		safe := agent.approvalFor(ctx, peerCall("send"), Hooks{})
		unsafe := agent.approvalFor(ctx, peerCall("send"), Hooks{})

		if safe.ask || !unsafe.ask || !strings.Contains(unsafe.reason, "holds a token") {
			t.Errorf("safe %+v unsafe %+v", safe, unsafe)
		}
		if asked := judge.requests[0].Messages[0].Text; !strings.Contains(asked, "is the token check done?") {
			t.Errorf("judged %q", asked)
		}
	})

	t.Run("PA-04 a list of peers never asks", func(t *testing.T) {
		agent, _ := peerAgent(t, permissions.ModeAsk, "ask", nil)

		if agent.approvalFor(ctx, peerCall("list"), Hooks{}).ask {
			t.Error("list asked")
		}
	})

	t.Run("PA-05 a message let in reaches the model once, marked as not the user's", func(t *testing.T) {
		agent, node := peerAgent(t, permissions.ModeAsk, "ask", nil)
		node.Deliver(peers.Message{From: peers.Info{ID: "k3f9x2qa", Name: "api@dev", Title: "Fix OAuth"}, Text: "token check is done", At: time.Now()})

		waiting := agent.HasEndedJobs()
		agent.tellEndedJobs()

		note := agent.History[len(agent.History)-1].Text
		if !waiting || agent.HasEndedJobs() || !strings.Contains(note, "api@dev (id k3f9x2qa") || !strings.Contains(note, "not from the user") || !strings.Contains(note, "token check is done") {
			t.Errorf("waiting %v note %q", waiting, note)
		}
	})

	t.Run("PA-06 incoming tools is worked out from the session's mode", func(t *testing.T) {
		got := map[permissions.Mode]string{}
		for _, mode := range permissions.Modes {
			agent, _ := peerAgent(t, mode, "tools", nil)
			got[mode] = agent.IncomingApproval("tools")
		}

		if got[permissions.ModeAsk] != "ask" || got[permissions.ModeEdits] != "ask" || got[permissions.ModeAuto] != "auto" || got[permissions.ModeAll] != "allow" {
			t.Errorf("got %v", got)
		}
	})
}
