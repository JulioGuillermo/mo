package peers

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | PN-01 | Happy    | Peers on the network at an address set list and message each other over TLS | P0     |
// | PN-02 | Security | A peer with another secret is refused both ways, said so to the sender      | P0       |
// | PN-03 | Error    | The network needs a secret of 16 characters or more; off leaves it          | P0       |
// | PN-04 | Security | A proof is bound to its TLS session: one from another connection fails      | P0       |
// | PN-05 | Security | A peer found has to show the certificate it told of                         | P0       |
// | PN-06 | Happy    | A peer of another computer is named with it; one name on two computers asks for the id | P1 |
// | PN-07 | Happy    | The same peer reached by socket and network is listed once, by its socket   | P1       |
// | PN-08 | Happy    | The same settings again keep the network as it is                           | P2       |

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

const testSecret = "a-team-secret-long-enough"

func networkNode(t *testing.T, folder, secret string, addresses ...string) *Node {
	t.Helper()
	node := startedNode(t, socketDir(t), folder, "dev")
	node.OnMessage(func(Message) {})
	if err := node.SetNetwork(Network{Mode: "hidden", Port: freePort(t), Secret: secret, Addresses: addresses}); err != nil {
		t.Fatal(err)
	}
	return node
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func addressOf(node *Node) string {
	return "127.0.0.1:" + strconv.Itoa(node.net.port)
}

func TestNetwork(t *testing.T) {
	ctx := context.Background()

	t.Run("PN-01 peers on the network at an address set list and message each other over TLS", func(t *testing.T) {
		api := networkNode(t, "/w/api", testSecret)
		var got []Message
		api.OnMessage(func(message Message) { got = append(got, message) })
		web := networkNode(t, "/w/web", testSecret, addressOf(api))

		found, err := web.List(ctx)
		_, sendErr := web.Send(ctx, "api@dev", "over the network")

		if err != nil || len(found) != 1 || found[0].ID != api.Self().ID || found[0].route.address == "" {
			t.Fatalf("found %+v err %v", found, err)
		}
		if sendErr != nil || len(got) != 1 || got[0].Text != "over the network" {
			t.Errorf("send %v got %+v", sendErr, got)
		}
	})

	t.Run("PN-02 a peer with another secret is refused both ways, said so to the sender", func(t *testing.T) {
		api := networkNode(t, "/w/api", testSecret)
		other := networkNode(t, "/w/web", "another-secret-of-the-same-size", addressOf(api))

		found, _ := other.List(ctx)
		_, err := other.ask(ctx, route{address: addressOf(api)}, request{Kind: "info"})

		if len(found) != 0 || err == nil || !strings.Contains(err.Error(), "secrets differ") {
			t.Errorf("found %v err %v", found, err)
		}
	})

	t.Run("PN-03 the network needs a secret of 16 characters or more; off leaves it", func(t *testing.T) {
		node := startedNode(t, socketDir(t), "/w/api", "")

		none := node.SetNetwork(Network{Mode: "visible", Secret: ""})
		short := node.SetNetwork(Network{Mode: "visible", Secret: "short"})
		node.SetNetwork(Network{Mode: "hidden", Secret: testSecret})
		on := node.NetworkState()
		node.SetNetwork(Network{Mode: "off"})

		if none == nil || short == nil || !strings.Contains(on, "network hidden") || node.NetworkState() != "this computer only" {
			t.Errorf("none %v short %v on %q off %q", none, short, on, node.NetworkState())
		}
	})

	t.Run("PN-04 a proof is bound to its TLS session: one from another connection fails", func(t *testing.T) {
		api := networkNode(t, "/w/api", testSecret)
		web := networkNode(t, "/w/web", testSecret)
		first, _ := web.net.dial(ctx, route{address: addressOf(api)})
		defer first.Close()
		earlier, _ := proofsOf(first, web.net.secret, clientSide)
		second, _ := web.net.dial(ctx, route{address: addressOf(api)})
		defer second.Close()
		now, _ := proofsOf(second, web.net.secret, clientSide)

		if earlier.own == "" || earlier.own == now.own || now.checks(earlier.expected) {
			t.Errorf("earlier %q now %q", earlier.own, now.own)
		}
	})

	t.Run("PN-05 a peer found has to show the certificate it told of", func(t *testing.T) {
		api := networkNode(t, "/w/api", testSecret)
		web := networkNode(t, "/w/web", testSecret)

		_, wrong := web.net.dial(ctx, route{address: addressOf(api), fingerprint: strings.Repeat("0", 64)})
		right, err := web.net.dial(ctx, route{address: addressOf(api), fingerprint: api.net.fingerprint})

		if wrong == nil || err != nil {
			t.Errorf("wrong %v right %v", wrong, err)
		}
		if right != nil {
			right.Close()
		}
	})

	t.Run("PN-06 a peer of another computer is named with it; one name on two computers asks for the id", func(t *testing.T) {
		node := NewNode(socketDir(t))
		node.Update(func(info *Info) { info.Host = "here" })
		laptop := Info{ID: "aaaaaaaa", Name: "api@dev", Host: "laptop"}
		desktop := Info{ID: "bbbbbbbb", Name: "api@dev", Host: "desktop"}

		label := node.Label(laptop)
		_, ambiguous := node.find([]Info{laptop, desktop}, "api@dev")
		byLabel, err := node.find([]Info{laptop, desktop}, "api@dev (desktop)")

		if label != "api@dev (laptop)" || ambiguous == nil || !strings.Contains(ambiguous.Error(), "use the id") || err != nil || byLabel.ID != "bbbbbbbb" {
			t.Errorf("label %q ambiguous %v byLabel %+v err %v", label, ambiguous, byLabel, err)
		}
	})

	t.Run("PN-07 the same peer reached by socket and network is listed once, by its socket", func(t *testing.T) {
		dir := socketDir(t)
		api := startedNode(t, dir, "/w/api", "dev")
		api.SetNetwork(Network{Mode: "hidden", Port: freePort(t), Secret: testSecret})
		web := startedNode(t, dir, "/w/web", "dev")
		web.SetNetwork(Network{Mode: "hidden", Secret: testSecret, Addresses: []string{addressOf(api)}})

		found, _ := web.List(ctx)

		if len(found) != 1 || found[0].route.socket == "" {
			t.Errorf("found %+v", found)
		}
	})

	t.Run("PN-08 the same settings again keep the network as it is", func(t *testing.T) {
		node := networkNode(t, "/w/api", testSecret)
		before := node.net

		node.SetNetwork(before.settings)

		if node.net != before {
			t.Error("the network started again")
		}
	})
}
