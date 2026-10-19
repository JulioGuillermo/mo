// Package peers lets mo instances of the same user find each other and pass
// messages: on one machine through a Unix socket each, in a folder only the
// user can open, and across the network over TLS, every connection proving
// a secret the team shares.
package peers

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	socketSuffix = ".sock"
	idLength     = 8
	dialTimeout  = time.Second
	// SendTimeout is how long a message waits to be received.
	SendTimeout  = 5 * time.Second
	listTimeout  = 2 * time.Second
	maxRequest   = 256 << 10
	MaxTextRunes = 20000
)

// Info is what an instance tells about itself, and how to reach it.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Dir     string `json:"dir,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Host    string `json:"host,omitempty"`
	Working bool   `json:"working"`
	// Since is when the instance started, to tell which of two with one
	// name came later.
	Since int64 `json:"since,omitempty"`
	route route
}

// startedBefore tells whether a started before b, the id deciding a tie.
func (a Info) startedBefore(b Info) bool {
	if a.Since != b.Since {
		return a.Since < b.Since
	}
	return a.ID < b.ID
}

// route is how a peer is reached: its socket on this machine, or its
// address on the network and, once found, the fingerprint of its
// certificate.
type route struct {
	socket      string
	address     string
	fingerprint string
}

// Message is what a peer sent.
type Message struct {
	From Info      `json:"from"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type request struct {
	Kind  string `json:"kind"`
	From  Info   `json:"from,omitempty"`
	Text  string `json:"text,omitempty"`
	Proof string `json:"proof,omitempty"`
}

type response struct {
	Info  *Info  `json:"info,omitempty"`
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
	Proof string `json:"proof,omitempty"`
}

// Node is this instance among its peers: where it listens, what it tells of
// itself, and the messages accepted for the model.
type Node struct {
	dir string

	mu        sync.Mutex
	self      Info
	fixedName string
	outgoing  string
	listener  net.Listener
	net       *network
	onMessage func(Message)
	onSent    func(to Info, text string)
	inbox     []Message
	seen      map[string]Info
}

// NewNode is an instance that listens in dir once started.
func NewNode(dir string) *Node {
	host, _ := os.Hostname()
	return &Node{dir: dir, seen: map[string]Info{}, self: Info{Host: host, Since: time.Now().UnixNano()}}
}

// SocketDir is where the instances of this user listen: the runtime
// folder, else one of the user's own in the temporary folder.
func SocketDir() string {
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return filepath.Join(runtime, "mo", "peers")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("mo-%d", os.Getuid()), "peers")
}

// Start listens for peers on this machine under a new id. Starting a
// started node does nothing.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.listener != nil {
		return nil
	}
	if err := privateDir(n.dir); err != nil {
		return err
	}
	return n.listenAsNew()
}

// listenAsNew takes a new id and listens on its socket; n.mu is held.
func (n *Node) listenAsNew() error {
	for range 5 {
		id := newID()
		listener, err := net.Listen("unix", n.socket(id))
		if errors.Is(err, syscall.EADDRINUSE) {
			continue
		}
		if err != nil {
			return fmt.Errorf("peers: %w", err)
		}
		n.listener = listener
		n.self.ID = id
		go n.serve(listener, nil)
		return nil
	}
	return errors.New("peers: no free id")
}

// changeID moves this instance to a new id, when another on the network
// has its own.
func (n *Node) changeID() {
	n.mu.Lock()
	defer n.mu.Unlock()
	old := n.listener
	if old == nil || n.listenAsNew() != nil {
		return
	}
	old.Close()
}

// Stop closes the socket and leaves the network; peers no longer find this
// instance.
func (n *Node) Stop() {
	n.mu.Lock()
	listener, network := n.listener, n.net
	n.listener, n.net = nil, nil
	n.mu.Unlock()
	if network != nil {
		network.stop()
	}
	if listener != nil {
		listener.Close()
	}
}

func (n *Node) Running() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.listener != nil
}

// Self is what this instance tells of itself.
func (n *Node) Self() Info {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.self
}

// Update changes what this instance tells of itself.
func (n *Node) Update(change func(info *Info)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	change(&n.self)
}

// Label is how a peer is named here: its name, with its computer when it
// runs on another.
func (n *Node) Label(info Info) string {
	if info.Host == "" || info.Host == n.Self().Host {
		return info.Name
	}
	return info.Name + " (" + info.Host + ")"
}

// SetName names this instance as the user asked, or by its folder and
// branch when name is empty, adding -2, -3 when another peer of this
// computer has it.
func (n *Node) SetName(ctx context.Context, name string) {
	n.mu.Lock()
	n.fixedName = name
	n.mu.Unlock()
	n.Rename(ctx)
}

// Rename works the name out again, after the folder or branch changed.
func (n *Node) Rename(ctx context.Context) {
	found, _ := n.List(ctx)
	n.nameAmong(found)
}

// nameAmong names this instance by its base name, adding -2, -3 when a
// peer of this computer that started before it has that name.
func (n *Node) nameAmong(found []Info) {
	n.mu.Lock()
	defer n.mu.Unlock()
	base := n.fixedName
	if base == "" {
		base = DefaultName(n.self.Dir, n.self.Branch)
	}
	taken := map[string]bool{}
	for _, other := range found {
		if n.sameComputer(other) && other.startedBefore(n.self) {
			taken[other.Name] = true
		}
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	n.self.Name = name
}

// sameComputer tells a peer of this computer, whose name has to differ;
// n.mu is held.
func (n *Node) sameComputer(other Info) bool {
	return other.Host == "" || other.Host == n.self.Host
}

// nameTaken tells whether a peer of this computer that started before
// this one has its name.
func (n *Node) nameTaken(found []Info) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, other := range found {
		if other.Name == n.self.Name && n.sameComputer(other) && other.startedBefore(n.self) {
			return true
		}
	}
	return false
}

// checkName takes another name when a peer found later turns out to have
// this one's, as one met over the network after both started.
func (n *Node) checkName() {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	n.List(ctx)
}

// DefaultName is folder@branch, or the folder alone outside a branch.
func DefaultName(dir, branch string) string {
	name := filepath.Base(dir)
	if branch != "" {
		name += "@" + branch
	}
	return name
}

// SetOutgoing says what asks before a message is sent: one of
// config.PeerApprovals.
func (n *Node) SetOutgoing(approval string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.outgoing = approval
}

func (n *Node) Outgoing() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.outgoing
}

// OnMessage says what to do with a message a peer sends; it is called on
// the node's own goroutine.
func (n *Node) OnMessage(handle func(Message)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onMessage = handle
}

// OnSent says what to do once a message was received by a peer.
func (n *Node) OnSent(handle func(to Info, text string)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onSent = handle
}

// List asks every peer what it is: those of this computer, and with the
// network on, those found there and at the addresses set. The local ones
// that no longer answer are forgotten, their sockets removed.
func (n *Node) List(ctx context.Context) ([]Info, error) {
	return n.list(ctx, whoWait)
}

// Scan is List waiting longer for the network's answers, to find the peers
// that are slow to answer or were not heard of yet.
func (n *Node) Scan(ctx context.Context) ([]Info, error) {
	return n.list(ctx, scanWait)
}

func (n *Node) list(ctx context.Context, answers time.Duration) ([]Info, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout+answers)
	defer cancel()
	routes, err := n.localRoutes()
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	network := n.net
	n.mu.Unlock()
	if network != nil {
		routes = append(routes, network.routes(ctx, answers)...)
	}
	self := n.Self().ID
	var (
		mu    sync.Mutex
		found []Info
		wait  sync.WaitGroup
	)
	for _, way := range routes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			answer, err := n.ask(ctx, way, request{Kind: "info"})
			if way.socket != "" && isGone(err) {
				os.Remove(way.socket)
				return
			}
			if err != nil || answer.Info == nil || answer.Info.ID == self {
				return
			}
			info := *answer.Info
			info.route = way
			mu.Lock()
			found = append(found, info)
			mu.Unlock()
		}()
	}
	wait.Wait()
	found = closestOnce(found)
	if n.nameTaken(found) {
		n.nameAmong(found)
	}
	slices.SortFunc(found, func(a, b Info) int { return strings.Compare(n.Label(a), n.Label(b)) })
	n.mu.Lock()
	for _, info := range found {
		n.seen[info.ID] = info
	}
	n.mu.Unlock()
	return found, nil
}

// closestOnce keeps each peer once: by its socket when it is on this
// computer too.
func closestOnce(found []Info) []Info {
	slices.SortStableFunc(found, func(a, b Info) int {
		if (a.route.socket != "") == (b.route.socket != "") {
			return 0
		}
		if a.route.socket != "" {
			return -1
		}
		return 1
	})
	seen := map[string]bool{}
	return slices.DeleteFunc(found, func(info Info) bool {
		if seen[info.ID] {
			return true
		}
		seen[info.ID] = true
		return false
	})
}

func (n *Node) localRoutes() ([]route, error) {
	entries, err := os.ReadDir(n.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	self := n.Self().ID
	var routes []route
	for _, entry := range entries {
		id, isSocket := strings.CutSuffix(entry.Name(), socketSuffix)
		if isSocket && id != self {
			routes = append(routes, route{socket: n.socket(id)})
		}
	}
	return routes, nil
}

// Send gives a message to the peer named, by name, name and computer, or
// id, and returns once it received it.
func (n *Node) Send(ctx context.Context, to, text string) (Info, error) {
	if !n.Running() {
		return Info{}, errors.New("peers are off in this mo")
	}
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return Info{}, errors.New("the message is empty")
	case len([]rune(text)) > MaxTextRunes:
		return Info{}, fmt.Errorf("the message is over %d characters", MaxTextRunes)
	}
	found, err := n.List(ctx)
	if err != nil {
		return Info{}, err
	}
	peer, err := n.find(found, to)
	if err != nil {
		return Info{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	answer, err := n.ask(ctx, peer.route, request{Kind: "message", From: n.Self(), Text: text})
	label := n.Label(peer)
	switch {
	case isGone(err):
		return peer, fmt.Errorf("%s is no longer running", label)
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return peer, fmt.Errorf("no answer from %s in %s", label, SendTimeout)
	case err != nil:
		return peer, fmt.Errorf("%s: %w", label, err)
	case answer.Error != "":
		return peer, fmt.Errorf("%s: %s", label, answer.Error)
	}
	n.mu.Lock()
	onSent := n.onSent
	n.mu.Unlock()
	if onSent != nil {
		onSent(peer, text)
	}
	return peer, nil
}

// find is the peer named: by id, by name and computer, then by name when
// one alone has it. A peer seen before and gone now is said to be gone.
func (n *Node) find(found []Info, to string) (Info, error) {
	to = strings.TrimSpace(to)
	var named []Info
	for _, peer := range found {
		switch {
		case peer.ID == to || n.Label(peer) == to:
			return peer, nil
		case strings.EqualFold(peer.Name, to):
			named = append(named, peer)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
	default:
		var labels []string
		for _, peer := range named {
			labels = append(labels, fmt.Sprintf("%s (id %s)", n.Label(peer), peer.ID))
		}
		return Info{}, fmt.Errorf("%d peers are named %q: use the id; %s", len(named), to, strings.Join(labels, ", "))
	}
	n.mu.Lock()
	var gone []Info
	for _, info := range n.seen {
		gone = append(gone, info)
	}
	n.mu.Unlock()
	for _, peer := range gone {
		if peer.ID != to && !strings.EqualFold(peer.Name, to) && n.Label(peer) != to {
			continue
		}
		if peer.route.socket != "" {
			return Info{}, fmt.Errorf("%s is no longer running", n.Label(peer))
		}
		return Info{}, fmt.Errorf("%s is not reachable: it stopped, or rejects the secret", n.Label(peer))
	}
	if len(found) == 0 {
		return Info{}, fmt.Errorf("no peer named %q: no other mo is running with peers on", to)
	}
	var labels []string
	for _, peer := range found {
		labels = append(labels, n.Label(peer))
	}
	return Info{}, fmt.Errorf("no peer named %q; peers: %s", to, strings.Join(labels, ", "))
}

// Deliver keeps a message the user let through, for the model's next step.
func (n *Node) Deliver(message Message) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.inbox = append(n.inbox, message)
}

// HasInbox tells whether messages wait for the model.
func (n *Node) HasInbox() bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.inbox) > 0
}

// TakeInbox gives the messages that wait for the model, once.
func (n *Node) TakeInbox() []Message {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	taken := n.inbox
	n.inbox = nil
	return taken
}

func (n *Node) socket(id string) string {
	return filepath.Join(n.dir, id+socketSuffix)
}

// serve answers what comes to a listener; secret, for the network, is what
// every request proves.
func (n *Node) serve(listener net.Listener, secret []byte) {
	busy := make(chan struct{}, maxConnections)
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		select {
		case busy <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-busy }()
			n.answer(conn, secret)
		}()
	}
}

func (n *Node) answer(conn net.Conn, secret []byte) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(SendTimeout))
	proofs, err := proofsOf(conn, secret, serverSide)
	if err != nil {
		return
	}
	var asked request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&asked); err != nil {
		return
	}
	if !proofs.checks(asked.Proof) {
		time.Sleep(authFailedDelay)
		json.NewEncoder(conn).Encode(response{Error: authFailed})
		return
	}
	answer := n.handle(asked)
	answer.Proof = proofs.own
	json.NewEncoder(conn).Encode(answer)
}

func (n *Node) handle(asked request) response {
	switch asked.Kind {
	case "info":
		self := n.Self()
		return response{Info: &self}
	case "message":
		n.mu.Lock()
		handle := n.onMessage
		n.mu.Unlock()
		if handle == nil {
			return response{Error: "it takes no messages now"}
		}
		handle(Message{From: asked.From, Text: asked.Text, At: time.Now()})
		return response{OK: true}
	}
	return response{Error: "unknown request " + asked.Kind}
}

// ask sends a request by its route and reads the answer: through the
// socket, or over TLS with the secret proved both ways.
func (n *Node) ask(ctx context.Context, way route, asked request) (response, error) {
	conn, secret, err := n.dial(ctx, way)
	if err != nil {
		return response{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	proofs, err := proofsOf(conn, secret, clientSide)
	if err != nil {
		return response{}, err
	}
	asked.Proof = proofs.own
	if err := json.NewEncoder(conn).Encode(asked); err != nil {
		return response{}, err
	}
	var answer response
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&answer); err != nil {
		return response{}, err
	}
	switch {
	case answer.Error == authFailed:
		return response{}, errors.New("authentication failed, the secrets differ")
	case !proofs.checks(answer.Proof):
		return response{}, errors.New("its answer did not prove the secret")
	}
	return answer, nil
}

func (n *Node) dial(ctx context.Context, way route) (net.Conn, []byte, error) {
	if way.socket != "" {
		dialer := net.Dialer{Timeout: dialTimeout}
		conn, err := dialer.DialContext(ctx, "unix", way.socket)
		return conn, nil, err
	}
	n.mu.Lock()
	network := n.net
	n.mu.Unlock()
	if network == nil {
		return nil, nil, errors.New("the network is off in this mo")
	}
	conn, err := network.dial(ctx, way)
	return conn, network.secret, err
}

// isGone tells an error of a socket or port nobody listens on.
func isGone(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func newID() string {
	raw := make([]byte, 5)
	rand.Read(raw)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))[:idLength]
}

// NewSecret is a random secret to share with a team, 32 characters.
func NewSecret() string {
	raw := make([]byte, 20)
	rand.Read(raw)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

// privateDir makes the folder the sockets go in, readable by the user
// alone, and refuses one another user owns.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("peers: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("peers: %w", err)
	}
	if !info.IsDir() || !ownedByUser(info) {
		return fmt.Errorf("peers: %s is not a folder of yours", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("peers: %w", err)
		}
	}
	return nil
}
