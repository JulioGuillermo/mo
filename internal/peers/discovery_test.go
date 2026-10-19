package peers

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PD-01 | Happy    | A visible instance answers who asks, a hidden one does not               | P0       |
// | PD-02 | Happy    | Here makes a peer seen at the sender's address; bye and time forget it   | P0       |
// | PD-03 | Security | Packets of another team, or no tag, are ignored                          | P0       |
// | PD-04 | Edge     | Its own packets are not a peer                                           | P1       |
// | PD-05 | Edge     | Another with its id makes the later of the two take a new one            | P1       |
// | PD-06 | Happy    | Two visible instances find each other by multicast, where the network has it | P1   |

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

type written struct {
	sent packet
	to   *net.UDPAddr
}

func fakeDiscovery(secret string, visible bool) (*discovery, *[]written) {
	var out []written
	d := newDiscovery([]byte(secret), visible, func() packet {
		return packet{Kind: "here", ID: "selfself", Port: 9000, FP: "own-fp", Since: 10}
	}, nil)
	d.write = func(sent packet, to *net.UDPAddr) { out = append(out, written{sent, to}) }
	return d, &out
}

func packetOf(d *discovery, sent packet) []byte {
	if sent.Tag == "" {
		sent.Tag = d.tag
	}
	data, _ := json.Marshal(sent)
	return data
}

var asker = &net.UDPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 50000}

func TestDiscovery(t *testing.T) {
	t.Run("PD-01 a visible instance answers who asks, a hidden one does not", func(t *testing.T) {
		visible, answered := fakeDiscovery(testSecret, true)
		hidden, silent := fakeDiscovery(testSecret, false)

		visible.handle(packetOf(visible, packet{Kind: "who"}), asker)
		hidden.handle(packetOf(hidden, packet{Kind: "who"}), asker)

		if len(*answered) != 1 || (*answered)[0].sent.Kind != "here" || (*answered)[0].to != asker || len(*silent) != 0 {
			t.Errorf("answered %+v silent %+v", *answered, *silent)
		}
	})

	t.Run("PD-02 here makes a peer seen at the sender's address; bye and time forget it", func(t *testing.T) {
		d, _ := fakeDiscovery(testSecret, false)
		now := time.Now()
		d.now = func() time.Time { return now }
		d.handle(packetOf(d, packet{Kind: "here", ID: "aaaaaaaa", Port: 7420, FP: "fp-a"}), asker)
		d.handle(packetOf(d, packet{Kind: "here", ID: "bbbbbbbb", Port: 7421, FP: "fp-b"}), asker)

		seen := d.sightings(context.Background(), 0)
		d.handle(packetOf(d, packet{Kind: "bye", FP: "fp-a"}), asker)
		now = now.Add(sightingTTL + time.Second)
		later := d.sightings(context.Background(), 0)

		if len(seen) != 2 || !strings.HasPrefix(seen[0].address, "192.168.1.20:742") || len(later) != 0 {
			t.Errorf("seen %+v later %+v", seen, later)
		}
	})

	t.Run("PD-03 packets of another team, or no tag, are ignored", func(t *testing.T) {
		d, out := fakeDiscovery(testSecret, true)
		other, _ := fakeDiscovery("another-secret-of-the-same-size", true)

		d.handle(packetOf(other, packet{Kind: "here", ID: "aaaaaaaa", Port: 7420, FP: "fp-a"}), asker)
		d.handle(packetOf(other, packet{Kind: "who"}), asker)
		untagged, _ := json.Marshal(packet{Kind: "who"})
		d.handle(untagged, asker)

		if len(d.sightings(context.Background(), 0)) != 0 || len(*out) != 0 {
			t.Errorf("seen something: %+v", *out)
		}
	})

	t.Run("PD-04 its own packets are not a peer", func(t *testing.T) {
		d, _ := fakeDiscovery(testSecret, true)

		d.handle(packetOf(d, d.self()), asker)

		if len(d.sightings(context.Background(), 0)) != 0 {
			t.Error("saw itself")
		}
	})

	t.Run("PD-05 another with its id makes the later of the two take a new one", func(t *testing.T) {
		later := startedNode(t, socketDir(t), "/w/api", "")
		later.SetNetwork(Network{Mode: "hidden", Port: freePort(t), Secret: testSecret})
		before := later.Self().ID
		d := later.net.discovery
		if d == nil {
			d = newDiscovery([]byte(testSecret), false, func() packet {
				return packet{ID: later.Self().ID, FP: later.net.fingerprint, Since: later.net.since}
			}, func(other packet) {
				if other.Since < later.net.since {
					later.changeID()
				}
			})
			d.write = func(packet, *net.UDPAddr) {}
		}

		d.handle(packetOf(d, packet{Kind: "here", ID: before, Port: 7420, FP: "someone-else", Since: 1}), asker)

		after := later.Self().ID
		if after == before {
			t.Fatal("kept its id")
		}
		if _, err := os.Stat(later.socket(after)); err != nil {
			t.Errorf("no socket for the new id: %v", err)
		}
	})

	t.Run("PD-06 two visible instances find each other by multicast, where the network has it", func(t *testing.T) {
		api := startedNode(t, socketDir(t), "/w/api", "dev")
		web := startedNode(t, socketDir(t), "/w/web", "dev")
		api.OnMessage(func(Message) {})
		for _, node := range []*Node{api, web} {
			if err := node.SetNetwork(Network{Mode: "visible", Secret: testSecret}); err != nil {
				t.Fatal(err)
			}
			if node.net.discovery == nil {
				t.Skip("no multicast here: " + node.net.problem)
			}
		}

		var found []Info
		for range 10 {
			found, _ = web.List(context.Background())
			if len(found) > 0 {
				break
			}
		}

		if len(found) == 0 {
			t.Skip("the network carries no multicast here")
		}
		if found[0].ID != api.Self().ID || found[0].route.fingerprint != api.net.fingerprint {
			t.Errorf("found %+v", found)
		}
	})
}
