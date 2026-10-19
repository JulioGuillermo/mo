package peers

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	groupAddress  = "239.255.77.79:7479"
	announceEvery = 30 * time.Second
	sightingTTL   = 75 * time.Second
	whoWait       = 300 * time.Millisecond
	scanWait      = 1500 * time.Millisecond
	maxPacket     = 1024
	announceLabel = "mo peers announce v1"
)

// packet is what goes over multicast: here, an instance telling where it
// takes peers; who, one asking who is there; bye, one leaving. Tag, signed
// with the secret, keeps the teams apart; it proves nothing, the TLS
// connection does.
type packet struct {
	Kind  string `json:"kind"`
	Tag   string `json:"tag"`
	ID    string `json:"id,omitempty"`
	Port  int    `json:"port,omitempty"`
	FP    string `json:"fp,omitempty"`
	Since int64  `json:"since,omitempty"`
}

type sighting struct {
	route route
	seen  time.Time
}

// discovery finds peers on the local network: it hears what they tell the
// multicast group and, visible, tells it of this instance and answers who
// asks.
type discovery struct {
	group    *net.UDPAddr
	listen   *net.UDPConn
	send     *net.UDPConn
	tag      string
	visible  bool
	self     func() packet
	onSameID func(packet)
	// onNew is told of a peer heard of for the first time.
	onNew func()
	write func(packet, *net.UDPAddr)
	now   func() time.Time

	mu      sync.Mutex
	found   map[string]sighting
	stopped chan struct{}
	once    sync.Once
}

func newDiscovery(secret []byte, visible bool, self func() packet, onSameID func(packet)) *discovery {
	d := &discovery{
		tag:      sign(secret, announceLabel)[:32],
		visible:  visible,
		self:     self,
		onSameID: onSameID,
		now:      time.Now,
		found:    map[string]sighting{},
		stopped:  make(chan struct{}),
	}
	d.write = d.writeUDP
	return d
}

func startDiscovery(secret []byte, visible bool, self func() packet, onSameID func(packet)) (*discovery, error) {
	d := newDiscovery(secret, visible, self, onSameID)
	group, err := net.ResolveUDPAddr("udp4", groupAddress)
	if err != nil {
		return nil, err
	}
	d.group = group
	d.listen, err = net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return nil, err
	}
	d.send, err = net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		d.listen.Close()
		return nil, err
	}
	go d.read(d.listen)
	go d.read(d.send)
	if visible {
		go d.announce()
	}
	return d, nil
}

func (d *discovery) stop() {
	d.once.Do(func() {
		close(d.stopped)
		if d.visible {
			d.write(packet{Kind: "bye", FP: d.self().FP}, d.group)
		}
		d.listen.Close()
		d.send.Close()
	})
}

func (d *discovery) read(conn *net.UDPConn) {
	buffer := make([]byte, maxPacket)
	for {
		size, from, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		d.handle(buffer[:size], from)
	}
}

func (d *discovery) announce() {
	ticker := time.NewTicker(announceEvery)
	defer ticker.Stop()
	for {
		d.write(d.self(), d.group)
		select {
		case <-d.stopped:
			return
		case <-ticker.C:
		}
	}
}

// handle takes a packet of the team: a peer seen or gone, or, visible, a
// question to answer.
func (d *discovery) handle(data []byte, from *net.UDPAddr) {
	var got packet
	if json.Unmarshal(data, &got) != nil || !hmac.Equal([]byte(got.Tag), []byte(d.tag)) {
		return
	}
	self := d.self()
	switch got.Kind {
	case "who":
		if d.visible {
			d.write(self, from)
		}
	case "here":
		if got.FP == "" || got.FP == self.FP || got.Port < 1 || got.Port > 65535 {
			return
		}
		if got.ID == self.ID && d.onSameID != nil {
			d.onSameID(got)
		}
		address := net.JoinHostPort(from.IP.String(), strconv.Itoa(got.Port))
		d.mu.Lock()
		_, known := d.found[got.FP]
		d.found[got.FP] = sighting{route: route{address: address, fingerprint: got.FP}, seen: d.now()}
		d.mu.Unlock()
		if !known && d.onNew != nil {
			go d.onNew()
		}
	case "bye":
		d.mu.Lock()
		delete(d.found, got.FP)
		d.mu.Unlock()
	}
}

// sightings asks who is there, waits a moment for the answers, and gives
// the peers seen lately.
func (d *discovery) sightings(ctx context.Context, wait time.Duration) []route {
	if d.group != nil {
		d.write(packet{Kind: "who"}, d.group)
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var routes []route
	for fingerprint, seen := range d.found {
		if d.now().Sub(seen.seen) > sightingTTL {
			delete(d.found, fingerprint)
			continue
		}
		routes = append(routes, seen.route)
	}
	return routes
}

func (d *discovery) writeUDP(sent packet, to *net.UDPAddr) {
	sent.Tag = d.tag
	data, err := json.Marshal(sent)
	if err != nil {
		return
	}
	d.send.WriteToUDP(data, to)
}
