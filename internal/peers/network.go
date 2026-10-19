package peers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strconv"
	"time"
)

const (
	// SecretMin is the shortest secret taken: the network sees what it
	// signs, so a short one could be guessed.
	SecretMin       = 16
	serverSide      = "mo peers server"
	clientSide      = "mo peers client"
	keyingLabel     = "EXPORTER-mo-peers-v1"
	authFailed      = "authentication failed"
	authFailedDelay = 500 * time.Millisecond
	maxConnections  = 32
	certificateLife = 365 * 24 * time.Hour
)

// Network is how this instance meets peers on other computers: Mode off,
// visible, found on the local network, or hidden, reached by address only.
type Network struct {
	Mode      string
	Port      int
	Secret    string
	Addresses []string
}

// network is the network side of a node: its TLS listener, its
// certificate's fingerprint, and the peers it finds.
type network struct {
	settings    Network
	secret      []byte
	listener    net.Listener
	port        int
	fingerprint string
	since       int64
	discovery   *discovery
	// problem says what does not work, as finding peers when the network
	// takes no multicast; the addresses set still do.
	problem string
}

// SetNetwork puts the network as the settings say, the node started:
// listening over TLS and, visible, telling the local network; off, it
// leaves. The same settings again change nothing.
func (n *Node) SetNetwork(settings Network) error {
	n.mu.Lock()
	current, running := n.net, n.listener != nil
	n.mu.Unlock()
	if current != nil && sameNetwork(current.settings, settings) {
		return nil
	}
	if current != nil {
		current.stop()
		n.mu.Lock()
		n.net = nil
		n.mu.Unlock()
	}
	switch {
	case settings.Mode == "" || settings.Mode == "off":
		return nil
	case !running:
		return errors.New("peers are off")
	case settings.Secret == "":
		return errors.New("the network needs a secret shared by the team: /peers secret new makes one")
	case len([]rune(settings.Secret)) < SecretMin:
		return fmt.Errorf("the secret is under %d characters: /peers secret new makes one", SecretMin)
	}
	started, err := n.startNetwork(settings)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.net = started
	n.mu.Unlock()
	return nil
}

func sameNetwork(a, b Network) bool {
	return a.Mode == b.Mode && a.Port == b.Port && a.Secret == b.Secret && slices.Equal(a.Addresses, b.Addresses)
}

// NetworkState says how this instance is on the network: off, or its mode,
// port and what does not work.
func (n *Node) NetworkState() string {
	n.mu.Lock()
	network := n.net
	n.mu.Unlock()
	if network == nil {
		return "this computer only"
	}
	state := fmt.Sprintf("network %s on port %d", network.settings.Mode, network.port)
	if network.problem != "" {
		state += " · " + network.problem
	}
	return state
}

func (n *Node) startNetwork(settings Network) (*network, error) {
	certificate, fingerprint, err := newCertificate()
	if err != nil {
		return nil, fmt.Errorf("peers: %w", err)
	}
	listener, err := tls.Listen("tcp", net.JoinHostPort("", strconv.Itoa(settings.Port)), &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		return nil, fmt.Errorf("peers: port %d: %w", settings.Port, err)
	}
	started := &network{
		settings:    settings,
		secret:      []byte(settings.Secret),
		listener:    listener,
		port:        listener.Addr().(*net.TCPAddr).Port,
		fingerprint: fingerprint,
		since:       time.Now().UnixNano(),
	}
	go n.serve(listener, started.secret)
	self := func() packet {
		return packet{Kind: "here", ID: n.Self().ID, Port: started.port, FP: fingerprint, Since: started.since}
	}
	onSameID := func(other packet) {
		if other.Since < started.since || (other.Since == started.since && other.FP < fingerprint) {
			n.changeID()
		}
	}
	started.discovery, err = startDiscovery(started.secret, settings.Mode == "visible", self, onSameID)
	if err != nil {
		started.problem = "peers are not found by multicast here (" + err.Error() + "), only at the addresses set"
	}
	return started, nil
}

func (w *network) stop() {
	if w.discovery != nil {
		w.discovery.stop()
	}
	w.listener.Close()
}

// routes are the peers to ask on the network: those found, and those at
// the addresses set.
func (w *network) routes(ctx context.Context, wait time.Duration) []route {
	var routes []route
	if w.discovery != nil {
		routes = w.discovery.sightings(ctx, wait)
	}
	for _, address := range w.settings.Addresses {
		routes = append(routes, route{address: address})
	}
	return routes
}

// dial opens TLS to a peer; a peer found has to show the certificate it
// told of. Who it is, the secret proves.
func (w *network) dial(ctx context.Context, way route) (net.Conn, error) {
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: dialTimeout}, Config: &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if way.fingerprint == "" {
				return nil
			}
			if len(state.PeerCertificates) == 0 || fingerprintOf(state.PeerCertificates[0].Raw) != way.fingerprint {
				return errors.New("its certificate is not the one it told of")
			}
			return nil
		},
	}}
	return dialer.DialContext(ctx, "tcp", way.address)
}

// proofs are what this side of a connection sends to prove the secret, and
// what the other side has to send; none outside TLS.
type proofs struct {
	own, expected string
}

func (p proofs) checks(proof string) bool {
	if p.expected == "" {
		return true
	}
	return hmac.Equal([]byte(proof), []byte(p.expected))
}

// proofsOf signs the keys of this TLS session with the secret, each side
// its own way: a proof holds for this connection alone, so one seen or
// passed on proves nothing elsewhere.
func proofsOf(conn net.Conn, secret []byte, side string) (proofs, error) {
	secured, isTLS := conn.(*tls.Conn)
	if !isTLS {
		return proofs{}, nil
	}
	if len(secret) == 0 {
		return proofs{}, errors.New("no secret")
	}
	if err := secured.Handshake(); err != nil {
		return proofs{}, err
	}
	state := secured.ConnectionState()
	material, err := state.ExportKeyingMaterial(keyingLabel, nil, 32)
	if err != nil {
		return proofs{}, err
	}
	other := clientSide
	if side == clientSide {
		other = serverSide
	}
	return proofs{own: sign(secret, side, material), expected: sign(secret, other, material)}, nil
}

func sign(secret []byte, parts ...any) string {
	mac := hmac.New(sha256.New, secret)
	for _, part := range parts {
		switch part := part.(type) {
		case string:
			mac.Write([]byte(part))
		case []byte:
			mac.Write(part)
		}
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// newCertificate is a certificate of this run alone, and its fingerprint.
func newCertificate() (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "mo peer"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(certificateLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, fingerprintOf(der), nil
}

func fingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
