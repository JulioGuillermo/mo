// Package peers lets mo instances of the same user on one machine find each
// other and pass messages, each through a Unix socket in a folder only the
// user can open.
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

// Info is what an instance tells about itself.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Dir     string `json:"dir,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Working bool   `json:"working"`
}

// Message is what a peer sent.
type Message struct {
	From Info      `json:"from"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type request struct {
	Kind string `json:"kind"`
	From Info   `json:"from,omitempty"`
	Text string `json:"text,omitempty"`
}

type response struct {
	Info  *Info  `json:"info,omitempty"`
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}

// Node is this instance among its peers: the socket it listens on, what it
// tells of itself, and the messages accepted for the model.
type Node struct {
	dir string

	mu        sync.Mutex
	self      Info
	fixedName string
	outgoing  string
	listener  net.Listener
	onMessage func(Message)
	onSent    func(to Info, text string)
	inbox     []Message
	seen      map[string]Info
}

// NewNode is an instance that listens in dir once started.
func NewNode(dir string) *Node {
	return &Node{dir: dir, seen: map[string]Info{}}
}

// SocketDir is where the instances of this user listen: the runtime
// folder, else one of the user's own in the temporary folder.
func SocketDir() string {
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return filepath.Join(runtime, "mo", "peers")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("mo-%d", os.Getuid()), "peers")
}

// Start listens for peers under a new id. Starting a started node does
// nothing.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.listener != nil {
		return nil
	}
	if err := privateDir(n.dir); err != nil {
		return err
	}
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
		go n.serve(listener)
		return nil
	}
	return errors.New("peers: no free id")
}

// Stop closes the socket; peers no longer find this instance.
func (n *Node) Stop() {
	n.mu.Lock()
	listener := n.listener
	n.listener = nil
	n.mu.Unlock()
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

// SetName names this instance as the user asked, or by its folder and
// branch when name is empty, adding -2, -3 when another peer has it.
func (n *Node) SetName(ctx context.Context, name string) {
	n.mu.Lock()
	n.fixedName = name
	n.mu.Unlock()
	n.Rename(ctx)
}

// Rename works the name out again, after the folder or branch changed.
func (n *Node) Rename(ctx context.Context) {
	n.mu.Lock()
	base := n.fixedName
	if base == "" {
		base = DefaultName(n.self.Dir, n.self.Branch)
	}
	n.mu.Unlock()
	taken := map[string]bool{}
	if others, err := n.List(ctx); err == nil {
		for _, other := range others {
			taken[other.Name] = true
		}
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	n.Update(func(info *Info) { info.Name = name })
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

// List asks every peer what it is; the ones that no longer answer are
// forgotten, their sockets removed.
func (n *Node) List(ctx context.Context) ([]Info, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	entries, err := os.ReadDir(n.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	self := n.Self().ID
	var (
		mu    sync.Mutex
		found []Info
		wait  sync.WaitGroup
	)
	for _, entry := range entries {
		id, isSocket := strings.CutSuffix(entry.Name(), socketSuffix)
		if !isSocket || id == self {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			answer, err := n.ask(ctx, id, request{Kind: "info"})
			if isGone(err) {
				os.Remove(n.socket(id))
				return
			}
			if err != nil || answer.Info == nil {
				return
			}
			answer.Info.ID = id
			mu.Lock()
			found = append(found, *answer.Info)
			mu.Unlock()
		}()
	}
	wait.Wait()
	slices.SortFunc(found, func(a, b Info) int { return strings.Compare(a.Name, b.Name) })
	n.mu.Lock()
	for _, info := range found {
		n.seen[info.ID] = info
	}
	n.mu.Unlock()
	return found, nil
}

// Send gives a message to the peer named, by name or id, and returns once
// it received it.
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
	peers, err := n.List(ctx)
	if err != nil {
		return Info{}, err
	}
	peer, found := find(peers, to)
	if !found {
		return Info{}, n.notFound(to, peers)
	}
	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	answer, err := n.ask(ctx, peer.ID, request{Kind: "message", From: n.Self(), Text: text})
	switch {
	case isGone(err):
		return peer, fmt.Errorf("%s is no longer running", peer.Name)
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return peer, fmt.Errorf("no answer from %s in %s", peer.Name, SendTimeout)
	case err != nil:
		return peer, err
	case answer.Error != "":
		return peer, fmt.Errorf("%s: %s", peer.Name, answer.Error)
	}
	n.mu.Lock()
	onSent := n.onSent
	n.mu.Unlock()
	if onSent != nil {
		onSent(peer, text)
	}
	return peer, nil
}

// notFound says a peer is gone when it was seen before, else which there
// are.
func (n *Node) notFound(to string, peers []Info) error {
	n.mu.Lock()
	seen := slices.Collect(func(yield func(Info) bool) {
		for _, info := range n.seen {
			if !yield(info) {
				return
			}
		}
	})
	n.mu.Unlock()
	if gone, wasSeen := find(seen, to); wasSeen {
		return fmt.Errorf("%s is no longer running", gone.Name)
	}
	if len(peers) == 0 {
		return fmt.Errorf("no peer named %q: no other mo is running with peers on", to)
	}
	var names []string
	for _, peer := range peers {
		names = append(names, peer.Name)
	}
	return fmt.Errorf("no peer named %q; peers: %s", to, strings.Join(names, ", "))
}

func find(peers []Info, to string) (Info, bool) {
	to = strings.TrimSpace(to)
	for _, peer := range peers {
		if peer.ID == to || peer.Name == to {
			return peer, true
		}
	}
	for _, peer := range peers {
		if strings.EqualFold(peer.Name, to) {
			return peer, true
		}
	}
	return Info{}, false
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

func (n *Node) serve(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go n.answer(conn)
	}
}

func (n *Node) answer(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(SendTimeout))
	var asked request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&asked); err != nil {
		return
	}
	var answer response
	switch asked.Kind {
	case "info":
		self := n.Self()
		answer.Info = &self
	case "message":
		n.mu.Lock()
		handle := n.onMessage
		n.mu.Unlock()
		if handle == nil {
			answer.Error = "it takes no messages now"
			break
		}
		handle(Message{From: asked.From, Text: asked.Text, At: time.Now()})
		answer.OK = true
	default:
		answer.Error = "unknown request " + asked.Kind
	}
	json.NewEncoder(conn).Encode(answer)
}

func (n *Node) ask(ctx context.Context, id string, asked request) (response, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", n.socket(id))
	if err != nil {
		return response{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(asked); err != nil {
		return response{}, err
	}
	var answer response
	err = json.NewDecoder(io.LimitReader(conn, maxRequest)).Decode(&answer)
	return answer, err
}

// isGone tells an error of a socket nobody listens on.
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
