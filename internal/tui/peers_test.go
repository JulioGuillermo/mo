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
// | PR-11 | Happy    | The settings keep the network, port, addresses and secret; a short secret or bad address is refused | P0 |
// | PR-12 | Security | A folder's secret goes to the user file under that folder, the others' kept  | P0       |
// | PR-13 | Happy    | Without a secret the network stays off and says so once                      | P1       |
// | PR-14 | Happy    | /peers secret new keeps a new secret in the user file and puts the network on | P1      |
// | PR-15 | Happy    | /peers visible, hidden and local change the network for the session only     | P1       |
// | PR-16 | Happy    | A session secret wins over the folder's, which wins over the global; clear goes back | P0 |
// | PR-17 | Security | A secret set is never shown nor kept in the input's history; a new one is shown once | P0 |
// | PR-18 | Error    | A short secret, or a scope that is not global, project or session, is refused | P1      |
// | PR-19 | Happy    | /peers scan asks the network, or says it is off and lists this computer's   | P2       |

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
		written, err := draft.peers(nil)

		if err != nil || !draft.peersEnabled || draft.peersIncoming != "auto" || !*written.Enabled || written.Incoming != "auto" || written.Outgoing != "allow" || written.Name != "backend" || written.Wake != nil {
			t.Errorf("draft %+v written %+v err %v", draft, written, err)
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

func TestPeerNetworkSettings(t *testing.T) {
	t.Run("PR-11 the settings keep the network, port, addresses and secret; a short secret or bad address is refused", func(t *testing.T) {
		draft := newSettingsDraft(config.Config{})
		draft.peersNetwork, draft.peersPort, draft.peersAddresses, draft.peersSecret = "visible", "7420", "10.0.0.2:7420\nbox.vpn:7420", "a-team-secret-long-enough"

		written, err := draft.peers(nil)
		draft.peersSecret = "short"
		_, short := draft.peers(nil)
		draft.peersSecret, draft.peersAddresses = "a-team-secret-long-enough", "no-port"
		_, address := draft.peers(nil)

		if err != nil || written.Network != "visible" || written.Port != 7420 || len(written.Addresses) != 2 || written.Secret != "a-team-secret-long-enough" {
			t.Errorf("written %+v err %v", written, err)
		}
		if short == nil || address == nil {
			t.Errorf("short %v address %v", short, address)
		}
	})

	t.Run("PR-12 a folder's secret goes to the user file under that folder, the others' kept", func(t *testing.T) {
		others := map[string]config.PeerProject{"/elsewhere": {Secret: "another-team-secret-here"}}
		layer := config.Config{Peers: config.Peers{Projects: others}}
		draft := newSettingsDraft(layer)
		draft.forProject("/w/api", layer)
		draft.projectSecret = "this-project-secret-long"

		written, err := draft.peers(layer.Peers.Projects)

		if err != nil || written.Projects["/w/api"].Secret != "this-project-secret-long" || written.Projects["/elsewhere"].Secret != "another-team-secret-here" {
			t.Errorf("projects %+v err %v", written.Projects, err)
		}
		if cfg := config.UserSettings(config.Config{Peers: written}); cfg.Peers.ForProject("/w/api").Key() != "this-project-secret-long" {
			t.Errorf("key here %q", cfg.Peers.ForProject("/w/api").Key())
		}
	})

	t.Run("PR-13 without a secret the network stays off and says so once", func(t *testing.T) {
		m := peerModel(t, "ask")
		m.session.Config.Peers.Network = "visible"
		m.session.Peers.Start()

		first := m.applyNetwork()
		again := m.applyNetwork()

		if first == nil || again != nil || !strings.Contains(m.peers.networkProblem, "needs a secret") || m.session.Peers.NetworkState() != "this computer only" {
			t.Errorf("problem %q state %q", m.peers.networkProblem, m.session.Peers.NetworkState())
		}
	})

	t.Run("PR-14 /peers secret new keeps a new secret in the user file and puts the network on", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		m := peerModel(t, "ask")
		m.session.Config.Peers.Network = "hidden"
		m.applyPeers()

		updated, _ := m.peersCommand("secret new")
		m = updated.(model)

		saved, _ := config.ReadUserLayer()
		if len(saved.Peers.Secret) < config.PeerSecretMin || m.session.Config.Peers.Secret != saved.Peers.Secret || !strings.Contains(shown(m), saved.Peers.Secret) {
			t.Fatalf("saved %q session %q", saved.Peers.Secret, m.session.Config.Peers.Secret)
		}
		if !strings.Contains(m.session.Peers.NetworkState(), "network hidden") {
			t.Errorf("state %q", m.session.Peers.NetworkState())
		}
	})

	t.Run("PR-15 /peers visible, hidden and local change the network for the session only", func(t *testing.T) {
		m := peerModel(t, "ask")
		m.session.Config.Peers.Secret = "a-team-secret-long-enough"
		m.applyPeers()

		updated, _ := m.peersCommand("hidden")
		m = updated.(model)
		hidden := m.session.Peers.NetworkState()
		updated, _ = m.peersCommand("local")
		m = updated.(model)

		if !strings.Contains(hidden, "network hidden") || m.session.Peers.NetworkState() != "this computer only" || m.session.Config.Peers.Network != "off" {
			t.Errorf("hidden %q local %q config %q", hidden, m.session.Peers.NetworkState(), m.session.Config.Peers.Network)
		}
	})
}

func TestPeerSecrets(t *testing.T) {
	const global, project, session = "the-global-team-secret", "this-folder-team-secret", "only-this-session-secret"

	t.Run("PR-16 a session secret wins over the folder's, which wins over the global; clear goes back", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		m := peerModel(t, "ask")
		var steps []string
		for _, command := range []string{"set global " + global, "set project " + project, "set session " + session, "clear session", "clear project"} {
			updated, _ := m.secretCommand(command)
			m = updated.(model)
			steps = append(steps, m.secretSource()+"="+m.peerSecret())
		}

		want := []string{"global=" + global, "project=" + project, "session=" + session, "project=" + project, "global=" + global}
		if strings.Join(steps, " ") != strings.Join(want, " ") {
			t.Errorf("steps %v", steps)
		}
		saved, _ := config.ReadUserLayer()
		if saved.Peers.Secret != global || saved.Peers.Projects != nil {
			t.Errorf("saved %+v", saved.Peers)
		}
	})

	t.Run("PR-17 a secret set is never shown nor kept in the input's history; a new one is shown once", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		m := peerModel(t, "ask")
		m.input.SetValue("/peers secret set session " + session)

		updated, _ := m.submit()
		m = updated.(model)
		updated, _ = m.secretCommand("new session")
		m = updated.(model)

		if strings.Contains(shown(m), session) || slices.Contains(m.history.entries, "/peers secret set session "+session) {
			t.Errorf("the secret leaked:\n%s\nhistory %v", shown(m), m.history.entries)
		}
		if !strings.Contains(shown(m), m.peers.secret) {
			t.Errorf("the new secret is not shown:\n%s", shown(m))
		}
	})

	t.Run("PR-18 a short secret, or a scope that is not global, project or session, is refused", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		m := peerModel(t, "ask")

		short, _ := m.secretCommand("set session short")
		scope, _ := m.secretCommand("set team " + session)

		if short.(model).peers.secret != "" || !strings.Contains(shown(short.(model)), "characters or more") || !strings.Contains(shown(scope.(model)), "use global, project or session") {
			t.Errorf("short:\n%s\nscope:\n%s", shown(short.(model)), shown(scope.(model)))
		}
	})

	t.Run("PR-19 /peers scan asks the network, or says it is off and lists this computer's", func(t *testing.T) {
		m := peerModel(t, "ask")
		m.applyPeers()
		off, _ := m.peersCommand("scan")
		m.peers.secret = session
		m.peers.network = "hidden"
		m.applyNetwork()

		on, _ := m.peersCommand("scan")

		if !strings.Contains(shown(off.(model)), "the network is off") || !strings.Contains(shown(on.(model)), "asking the network") {
			t.Errorf("off:\n%s\non:\n%s", shown(off.(model)), shown(on.(model)))
		}
	})
}
