package tui

// Test Traceability Table
// | ID    | Category | Description                                                                  | Priority |
// | PR-01 | Happy    | Incoming allow shows the message in its block and gives it to the model, waking it | P0 |
// | PR-02 | Happy    | Incoming ask shows it waiting and asks; let in reaches the model, rejected not | P0     |
// | PR-03 | Happy    | Letting in all from a peer lets its next message in unasked                  | P1       |
// | PR-04 | Security | After 10 messages in a row without the user, the next asks; a prompt resets it | P0     |
// | PR-05 | Happy    | Closing the question leaves the message waiting; /peers asks again          | P1       |
// | PR-06 | Happy    | A message sent shows in its own block, its bar a colour apart from incoming | P1       |
// | PR-07 | Happy    | /peers incoming, outgoing and name change the session only; off stops it     | P1       |
// | PR-08 | Happy    | Wake off leaves the model idle with the message waiting                     | P2       |
// | PR-09 | Happy    | The settings screen reads and writes the peers' settings                     | P1       |
// | PR-10 | Happy    | name auto takes a few words of the title                                     | P2       |

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/peers"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

func peerModel(t *testing.T, incoming string) model {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "mo-peers-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	node := peers.NewNode(filepath.Join(root, "peers"))
	t.Cleanup(node.Stop)
	policy, _ := permissions.New(permissions.ModeEdits, nil, nil)
	m := conversationModel()
	m.session.Peers = node
	m.session.Config.Peers.Enabled = ptrTo(true)
	m.session.Config.Peers.Incoming = incoming
	m.session.Agent = &agent.Agent{Tools: tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Peers: node}, tools.Options{}), Permissions: policy}
	m.peers = newPeerSession()
	return m
}

func ptrTo[T any](value T) *T {
	return &value
}

func fromPeer(text string) peers.Message {
	return peers.Message{From: peers.Info{ID: "k3f9x2qa", Name: "api@dev", Title: "Fix OAuth"}, Text: text, At: time.Now()}
}

func arrive(m model, text string) model {
	updated, _ := m.Update(peerMsg(fromPeer(text)))
	return updated.(model)
}

func TestPeerMessages(t *testing.T) {
	t.Run("PR-01 incoming allow shows the message in its block and gives it to the model, waking it", func(t *testing.T) {
		m := peerModel(t, "allow")

		m = arrive(m, "the token check is done")

		screen := shown(m)
		if !strings.Contains(screen, `⇠ api@dev · "Fix OAuth"`) || !strings.Contains(screen, "the token check is done") || m.popup != nil {
			t.Errorf("screen:\n%s", screen)
		}
		if !m.session.Peers.HasInbox() || !m.running {
			t.Errorf("inbox %v running %v", m.session.Peers.HasInbox(), m.running)
		}
	})

	t.Run("PR-02 incoming ask shows it waiting and asks; let in reaches the model, rejected not", func(t *testing.T) {
		m := peerModel(t, "ask")
		m = arrive(m, "first")
		waiting := strings.Contains(shown(m), "waiting for you") && m.popup != nil && m.popup.peer && !m.session.Peers.HasInbox()

		updated, _ := m.choosePopup(0)
		m = updated.(model)
		letIn := len(m.session.Peers.TakeInbox()) == 1
		m.running = true
		m = arrive(m, "second")
		updated, _ = m.choosePopup(2)
		m = updated.(model)

		if !waiting || !letIn || m.session.Peers.HasInbox() || !strings.Contains(shown(m), "message from api@dev rejected") {
			t.Errorf("waiting %v letIn %v screen:\n%s", waiting, letIn, shown(m))
		}
	})

	t.Run("PR-03 letting in all from a peer lets its next message in unasked", func(t *testing.T) {
		m := peerModel(t, "ask")
		m.running = true
		m = arrive(m, "first")
		updated, _ := m.choosePopup(1)
		m = updated.(model)

		m = arrive(m, "second")

		if m.popup != nil || len(m.session.Peers.TakeInbox()) != 2 {
			t.Errorf("popup %v", m.popup != nil)
		}
	})

	t.Run("PR-04 after 10 messages in a row without the user, the next asks; a prompt resets it", func(t *testing.T) {
		m := peerModel(t, "allow")
		m.running = true
		for i := range peerStreakMost {
			m = arrive(m, fmt.Sprint("message ", i))
		}
		quiet := m.popup == nil

		m = arrive(m, "one too many")
		asked := m.popup != nil && strings.Contains(m.popup.description, "10 messages from peers in a row")
		updated, _ := m.choosePopup(0)
		m = updated.(model)
		m.peers.streak = peerStreakMost
		m.input.SetValue("go on")
		updated, _ = m.submit()
		m = updated.(model)

		if !quiet || !asked || m.peers.streak != 0 {
			t.Errorf("quiet %v asked %v streak %d", quiet, asked, m.peers.streak)
		}
	})

	t.Run("PR-05 closing the question leaves the message waiting; /peers asks again", func(t *testing.T) {
		m := peerModel(t, "ask")
		m = arrive(m, "later")

		updated, _ := m.dismissPopup()
		m = updated.(model)
		closed := m.popup == nil && len(m.peers.pending) == 1
		updated, _ = m.peersCommand("")
		m = updated.(model)

		if !closed || m.popup == nil || !m.popup.peer {
			t.Errorf("closed %v popup %v", closed, m.popup != nil)
		}
	})

	t.Run("PR-06 a message sent shows in its own block, its bar a colour apart from incoming", func(t *testing.T) {
		m := peerModel(t, "allow")
		m.running = true
		m = arrive(m, "question")

		updated, _ := m.Update(peerSentMsg{to: peers.Info{Name: "api@dev"}, text: "the answer"})
		m = updated.(model)

		colors := peerPalette(true)
		blocks := m.blocks[len(m.blocks)-2:]
		if !strings.Contains(ansi.Strip(blocks[1].text), "⇢ to api@dev") || !strings.Contains(blocks[1].text, foreground(colors.outgoing)) || !strings.Contains(blocks[0].text, foreground(colors.incoming)) {
			t.Errorf("blocks:\n%q\n%q", blocks[0].text, blocks[1].text)
		}
	})

	t.Run("PR-07 /peers incoming, outgoing and name change the session only; off stops it", func(t *testing.T) {
		m := peerModel(t, "ask")
		m.applyPeers()
		started := m.session.Peers.Running()

		for _, command := range []string{"incoming allow", "outgoing ask", "name backend", "off"} {
			updated, _ := m.peersCommand(command)
			m = updated.(model)
		}

		if !started || m.incomingSetting() != "allow" || m.session.Peers.Outgoing() != "ask" || m.peerName() != "backend" || m.session.Config.Peers.Incoming != "ask" {
			t.Errorf("started %v incoming %q outgoing %q name %q", started, m.incomingSetting(), m.session.Peers.Outgoing(), m.peerName())
		}
		if m.session.Peers.Running() {
			t.Error("still running after off")
		}
	})

	t.Run("PR-08 wake off leaves the model idle with the message waiting", func(t *testing.T) {
		m := peerModel(t, "allow")
		m.session.Config.Peers.Wake = ptrTo(false)

		m = arrive(m, "no hurry")

		if m.running || !m.session.Peers.HasInbox() {
			t.Errorf("running %v", m.running)
		}
	})

	t.Run("PR-09 the settings screen reads and writes the peers' settings", func(t *testing.T) {
		draft := newSettingsDraft(config.Config{Peers: config.Peers{Enabled: ptrTo(true), Incoming: "auto"}})

		draft.peersOutgoing, draft.peersName = "allow", "backend"
		written := draft.peers()

		if !draft.peersEnabled || draft.peersIncoming != "auto" || !*written.Enabled || written.Incoming != "auto" || written.Outgoing != "allow" || written.Name != "backend" || written.Wake != nil {
			t.Errorf("draft %+v written %+v", draft, written)
		}
	})

	t.Run("PR-10 name auto takes a few words of the title", func(t *testing.T) {
		m := peerModel(t, "ask")
		m.thread.title = "Arreglar el login OAuth, por favor"
		m.peers.name = ptrTo(config.PeerNameAuto)

		if name := m.peerName(); name != "arreglar-el-login" {
			t.Errorf("name = %q", name)
		}
	})
}

func foreground(c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}
