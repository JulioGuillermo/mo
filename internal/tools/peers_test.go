package tools

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | PT-01 | Happy    | list names this instance and the others, with state, folder and title | P0       |
// | PT-02 | Happy    | send says delivered; a peer that is gone says so, compactly           | P0       |
// | PT-03 | Edge     | Without peers there is no tool; stopped, it says peers are off        | P1       |

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/peers"
)

func peerNodes(t *testing.T) (*peers.Node, *peers.Node) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "mo-peers-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	dir := filepath.Join(root, "peers")
	start := func(folder, title string) *peers.Node {
		node := peers.NewNode(dir)
		node.Update(func(info *peers.Info) { info.Dir, info.Branch, info.Title = folder, "dev", title })
		if err := node.Start(); err != nil {
			t.Fatal(err)
		}
		node.OnMessage(func(peers.Message) {})
		node.Rename(context.Background())
		t.Cleanup(node.Stop)
		return node
	}
	return start("/w/web", ""), start("/w/api", "Fix OAuth")
}

func runPeers(t *testing.T, node *peers.Node, args map[string]any) (string, bool) {
	t.Helper()
	registry := NewRegistry(Env{WorkDir: t.TempDir(), Peers: node}, Options{})
	raw, _ := json.Marshal(args)
	return registry.Run(context.Background(), llm.ToolCall{Name: PeersName, Args: raw})
}

func TestPeersTool(t *testing.T) {
	t.Run("PT-01 list names this instance and the others, with state, folder and title", func(t *testing.T) {
		web, api := peerNodes(t)
		api.Update(func(info *peers.Info) { info.Working = true })

		output, isError := runPeers(t, web, map[string]any{"action": "list"})

		if isError || !strings.Contains(output, "you are web@dev") || !strings.Contains(output, `api@dev (id `+api.Self().ID+`) · working · /w/api · "Fix OAuth"`) {
			t.Errorf("output:\n%s", output)
		}
	})

	t.Run("PT-02 send says delivered; a peer that is gone says so, compactly", func(t *testing.T) {
		web, api := peerNodes(t)

		sent, sentError := runPeers(t, web, map[string]any{"action": "send", "to": "api@dev", "text": "done?"})
		api.Stop()
		gone, goneError := runPeers(t, web, map[string]any{"action": "send", "to": "api@dev", "text": "still there?"})

		if sentError || sent != "delivered to api@dev" {
			t.Errorf("sent %q", sent)
		}
		if !goneError || !strings.Contains(gone, "not sent: api@dev is no longer running") {
			t.Errorf("gone %q", gone)
		}
	})

	t.Run("PT-03 without peers there is no tool; stopped, it says peers are off", func(t *testing.T) {
		without := NewRegistry(Env{WorkDir: t.TempDir()}, Options{})
		stopped := peers.NewNode(t.TempDir())

		output, isError := runPeers(t, stopped, map[string]any{"action": "list"})

		if _, found := without.find(PeersName); found {
			t.Error("the tool is there without peers")
		}
		if !isError || !strings.Contains(output, "peers are off") {
			t.Errorf("output %q", output)
		}
	})
}
