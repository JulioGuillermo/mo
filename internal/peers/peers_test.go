package peers

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PE-01 | Happy    | Started nodes list each other, not themselves, with what they tell       | P0       |
// | PE-02 | Happy    | A message sent by name reaches the other's handler, and the sender hears so | P0    |
// | PE-03 | Error    | A peer that stopped is no longer running; an unknown name lists the others | P0     |
// | PE-04 | Happy    | A name another peer has gets -2                                          | P1       |
// | PE-05 | Happy    | The inbox gives the messages delivered once                              | P1       |
// | PE-06 | Security | The sockets' folder is the user's alone                                  | P0       |
// | PE-07 | Error    | An empty or too long message, or a stopped node, sends nothing            | P1       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func startedNode(t *testing.T, dir, folder, branch string) *Node {
	t.Helper()
	node := NewNode(dir)
	node.Update(func(info *Info) { info.Dir, info.Branch = folder, branch })
	if err := node.Start(); err != nil {
		t.Fatal(err)
	}
	node.Rename(context.Background())
	t.Cleanup(node.Stop)
	return node
}

func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mo-peers-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "peers")
}

func TestPeers(t *testing.T) {
	ctx := context.Background()

	t.Run("PE-01 started nodes list each other, not themselves, with what they tell", func(t *testing.T) {
		dir := socketDir(t)
		backend := startedNode(t, dir, "/w/api", "feat-auth")
		front := startedNode(t, dir, "/w/web", "main")
		backend.Update(func(info *Info) { info.Title, info.Working = "Fix OAuth", true })

		found, err := front.List(ctx)

		if err != nil || len(found) != 1 {
			t.Fatalf("found %v err %v", found, err)
		}
		if got := found[0]; got.Name != "api@feat-auth" || got.Title != "Fix OAuth" || !got.Working || got.ID != backend.Self().ID {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("PE-02 a message sent by name reaches the other's handler, and the sender hears so", func(t *testing.T) {
		dir := socketDir(t)
		backend := startedNode(t, dir, "/w/api", "dev")
		front := startedNode(t, dir, "/w/web", "main")
		var mu sync.Mutex
		var received []Message
		backend.OnMessage(func(message Message) {
			mu.Lock()
			received = append(received, message)
			mu.Unlock()
		})
		var sentTo string
		front.OnSent(func(to Info, _ string) { sentTo = to.Name })

		peer, err := front.Send(ctx, "api@dev", "did ValidateToken change?")

		mu.Lock()
		defer mu.Unlock()
		if err != nil || peer.ID != backend.Self().ID || sentTo != "api@dev" {
			t.Fatalf("peer %+v sentTo %q err %v", peer, sentTo, err)
		}
		if len(received) != 1 || received[0].Text != "did ValidateToken change?" || received[0].From.Name != "web@main" {
			t.Errorf("received %+v", received)
		}
	})

	t.Run("PE-03 a peer that stopped is no longer running; an unknown name lists the others", func(t *testing.T) {
		dir := socketDir(t)
		backend := startedNode(t, dir, "/w/api", "dev")
		startedNode(t, dir, "/w/db", "")
		front := startedNode(t, dir, "/w/web", "main")
		front.List(ctx)
		backend.Stop()

		_, gone := front.Send(ctx, "api@dev", "hi")
		_, unknown := front.Send(ctx, "nobody", "hi")

		if gone == nil || !strings.Contains(gone.Error(), "api@dev is no longer running") {
			t.Errorf("gone: %v", gone)
		}
		if unknown == nil || !strings.Contains(unknown.Error(), `no peer named "nobody"; peers: db`) {
			t.Errorf("unknown: %v", unknown)
		}
	})

	t.Run("PE-04 a name another peer has gets -2", func(t *testing.T) {
		dir := socketDir(t)
		startedNode(t, dir, "/w/api", "dev")
		second := startedNode(t, dir, "/other/api", "dev")

		if name := second.Self().Name; name != "api@dev-2" {
			t.Errorf("name = %q", name)
		}
	})

	t.Run("PE-05 the inbox gives the messages delivered once", func(t *testing.T) {
		node := NewNode(socketDir(t))
		node.Deliver(Message{Text: "one", At: time.Now()})

		first, second := node.TakeInbox(), node.TakeInbox()

		if len(first) != 1 || len(second) != 0 || node.HasInbox() {
			t.Errorf("first %v second %v", first, second)
		}
	})

	t.Run("PE-06 the sockets' folder is the user's alone", func(t *testing.T) {
		dir := socketDir(t)
		os.MkdirAll(dir, 0o777)
		os.Chmod(dir, 0o777)

		startedNode(t, dir, "/w/api", "")

		info, _ := os.Stat(dir)
		if info.Mode().Perm() != 0o700 {
			t.Errorf("mode %v", info.Mode().Perm())
		}
	})

	t.Run("PE-07 an empty or too long message, or a stopped node, sends nothing", func(t *testing.T) {
		dir := socketDir(t)
		startedNode(t, dir, "/w/api", "")
		front := startedNode(t, dir, "/w/web", "")

		_, empty := front.Send(ctx, "api", "  ")
		_, long := front.Send(ctx, "api", strings.Repeat("x", MaxTextRunes+1))
		front.Stop()
		_, stopped := front.Send(ctx, "api", "hi")

		if empty == nil || long == nil || stopped == nil || !strings.Contains(stopped.Error(), "off") {
			t.Errorf("empty %v long %v stopped %v", empty, long, stopped)
		}
	})
}
