package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/peers"
)

const (
	peerStreakMost  = 10
	peerRetry       = time.Second
	peerNameWords   = 3
	peerListTimeout = 3 * time.Second
	peersUsage      = "/peers · name [<name>|auto] · incoming|outgoing tools|ask|auto|allow · on|off"
)

// peerMsg is a message a peer sent; peerSentMsg one this agent sent;
// peerJudgedMsg the model's judgement of one; peerRetryMsg a try again to
// ask about the messages that wait.
type peerMsg peers.Message

type peerSentMsg struct {
	to   peers.Info
	text string
}

type peerJudgedMsg struct {
	message peers.Message
	safe    bool
	reason  string
}

type peerRetryMsg struct{}

type peersListedMsg struct {
	text string
	err  error
}

// peerSession is what the user changed of the peers for this session with
// /peers, and the messages that wait for the user.
type peerSession struct {
	name     *string
	incoming string
	outgoing string
	off      bool
	// streak is how many messages in a row came in without the user;
	// trusted the peers the user let in for the session, by id.
	streak  int
	trusted map[string]bool
	// pending wait for the user's answer, the first asked about unless
	// postponed, when the user closed the question.
	pending   []pendingPeer
	postponed bool
	retrying  bool
}

type pendingPeer struct {
	message peers.Message
	reason  string
}

func newPeerSession() *peerSession {
	return &peerSession{trusted: map[string]bool{}}
}

// followPeers tells the program of the messages peers send and this agent
// sends them.
func followPeers(node *peers.Node, bridge *bridge) {
	if node == nil {
		return
	}
	node.OnMessage(func(message peers.Message) { bridge.send(peerMsg(message)) })
	node.OnSent(func(to peers.Info, text string) { bridge.send(peerSentMsg{to: to, text: text}) })
}

// peersOn tells whether the settings and the session have peers on.
func (m model) peersOn() bool {
	return m.session.Peers != nil && m.peers != nil && *m.session.Config.Peers.Enabled && !m.peers.off
}

// applyPeers starts or stops this instance among its peers as the
// settings and /peers say, and tells what it is.
func (m model) applyPeers() tea.Cmd {
	node := m.session.Peers
	if node == nil || m.peers == nil {
		return nil
	}
	if !m.peersOn() {
		node.Stop()
		return nil
	}
	node.SetOutgoing(orDefault(m.peers.outgoing, m.session.Config.Peers.Outgoing))
	m.tellPeers()
	if err := node.Start(); err != nil {
		return printText(m.printer.Error(err))
	}
	return m.renamePeer()
}

// tellPeers updates what this instance tells its peers: the folder, the
// branch, the conversation's title and whether the model works.
func (m model) tellPeers() {
	if m.session.Peers == nil {
		return
	}
	m.session.Peers.Update(func(info *peers.Info) {
		info.Dir, info.Branch, info.Title, info.Working = m.session.WorkDir, m.branch, m.thread.title, m.running
	})
}

// peersAfterTurn tells the peers the title the turn may have given the
// conversation, and takes a name from it when the name is auto.
func (m model) peersAfterTurn() tea.Cmd {
	if m.peers == nil || !m.peersOn() {
		return nil
	}
	m.tellPeers()
	name := m.session.Config.Peers.Name
	if m.peers.name != nil {
		name = *m.peers.name
	}
	if name != config.PeerNameAuto {
		return nil
	}
	return m.renamePeer()
}

// renamePeer works out this instance's name off the UI goroutine, since
// it asks the others theirs.
func (m model) renamePeer() tea.Cmd {
	node := m.session.Peers
	if node == nil || !m.peersOn() {
		return nil
	}
	name := m.peerName()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), peerListTimeout)
		defer cancel()
		node.SetName(ctx, name)
		return nil
	}
}

// peerName is the name asked for: /peers name, else the settings; auto
// takes a few words of the title; empty is folder@branch.
func (m model) peerName() string {
	name := m.session.Config.Peers.Name
	if m.peers.name != nil {
		name = *m.peers.name
	}
	if name == config.PeerNameAuto {
		return slug(m.thread.title)
	}
	return name
}

// slug is the first words of a title, lower case, joined by dashes.
func slug(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return strings.Join(words[:min(len(words), peerNameWords)], "-")
}

func (m model) incomingSetting() string {
	return orDefault(m.peers.incoming, m.session.Config.Peers.Incoming)
}

// peerArrived decides on a message a peer sent: let in, asked about, or
// judged first, as the incoming setting and the session's mode say.
func (m model) peerArrived(message peers.Message) (tea.Model, tea.Cmd) {
	if m.peers == nil || m.session.Agent == nil {
		return m, nil
	}
	approval := m.session.Agent.IncomingApproval(m.incomingSetting())
	switch {
	case approval == "allow" || m.peers.trusted[message.From.ID]:
		return m.letPeerInUnasked(message, "")
	case approval == "auto":
		assistant := m.session.Agent
		return m, func() tea.Msg {
			safe, reason := assistant.JudgeIncoming(context.Background(), message)
			return peerJudgedMsg{message: message, safe: safe, reason: reason}
		}
	}
	return m.askAboutPeer(message, "")
}

func (m model) peerJudged(msg peerJudgedMsg) (tea.Model, tea.Cmd) {
	if msg.safe {
		return m.letPeerInUnasked(msg.message, "auto: "+msg.reason)
	}
	return m.askAboutPeer(msg.message, "auto: "+msg.reason)
}

// letPeerInUnasked lets a message in without the user, unless too many
// came so in a row.
func (m model) letPeerInUnasked(message peers.Message, status string) (tea.Model, tea.Cmd) {
	if m.peers.streak >= peerStreakMost {
		return m.askAboutPeer(message, fmt.Sprintf("%d messages from peers in a row without you", peerStreakMost))
	}
	m.peers.streak++
	return m.letPeerIn(message, status)
}

// letPeerIn gives the model the message: at its next step while it works,
// else in a turn of its own when the settings wake it.
func (m model) letPeerIn(message peers.Message, status string) (tea.Model, tea.Cmd) {
	m.session.Peers.Deliver(message)
	shown, show := m.emit(m.printer.PeerIn(message, status))
	m = shown.(model)
	if m.running || !*m.session.Config.Peers.Wake {
		return m, show
	}
	woken, wake := m.reactToJobs()
	return woken, tea.Batch(show, wake)
}

// askAboutPeer shows the message, waiting, and asks the user whether to
// let it in.
func (m model) askAboutPeer(message peers.Message, reason string) (tea.Model, tea.Cmd) {
	status := "waiting for you"
	if reason != "" {
		status += " · " + reason
	}
	m.peers.pending = append(m.peers.pending, pendingPeer{message: message, reason: reason})
	m.peers.postponed = false
	shown, show := m.emit(m.printer.PeerIn(message, status))
	m = shown.(model)
	asked, ask := m.askNextPeer()
	return asked, tea.Batch(show, ask)
}

// askNextPeer opens the question about the first message that waits, or
// tries again in a moment while something else is open.
func (m model) askNextPeer() (tea.Model, tea.Cmd) {
	p := m.peers
	if len(p.pending) == 0 || p.postponed || (m.popup != nil && m.popup.peer) {
		return m, nil
	}
	if m.popup != nil || m.dialog != nil || m.settings != nil {
		if p.retrying {
			return m, nil
		}
		p.retrying = true
		return m, tea.Tick(peerRetry, func(time.Time) tea.Msg { return peerRetryMsg{} })
	}
	waiting := p.pending[0]
	from := waiting.message.From.Name
	description := "Its message is above, in the conversation."
	if waiting.reason != "" {
		description = waiting.reason + ". " + description
	}
	if len(p.pending) > 1 {
		description += fmt.Sprintf(" %d more wait.", len(p.pending)-1)
	}
	m.popup = &popup{
		title:       "⇠ message from " + from,
		description: description,
		options: []huh.Option[string]{
			huh.NewOption("Let it in", "in"),
			huh.NewOption("Let in all from "+from+" this session", "trust"),
			huh.NewOption("Reject it · "+from+" is not told", "reject"),
		},
		border: peerPalette(m.printer.isDark()).incoming,
		peer:   true,
		do: func(m model, choice string) (tea.Model, tea.Cmd) {
			return m.answerPeer(choice)
		},
		dismiss: func(m model) (tea.Model, tea.Cmd) {
			m.peers.postponed = true
			return m.emit(m.printer.Note("a peer's message waits for you · /peers answers it"))
		},
	}
	return m, nil
}

func (m model) peerRetried() (tea.Model, tea.Cmd) {
	m.peers.retrying = false
	return m.askNextPeer()
}

// answerPeer does what the user chose for the first message that waits,
// then asks about the next.
func (m model) answerPeer(choice string) (tea.Model, tea.Cmd) {
	p := m.peers
	if len(p.pending) == 0 {
		return m, nil
	}
	waiting := p.pending[0]
	p.pending = p.pending[1:]
	from := waiting.message.From
	var done tea.Cmd
	if choice == "reject" {
		var shown tea.Model
		shown, done = m.emit(m.printer.Note("message from " + from.Name + " rejected · it is not told"))
		m = shown.(model)
	} else {
		p.streak = 0
		if choice == "trust" {
			p.trusted[from.ID] = true
		}
		m.session.Peers.Deliver(waiting.message)
		note := "message from " + from.Name + " let in"
		if choice == "trust" {
			note += " · and all it sends this session"
		}
		var shown tea.Model
		shown, done = m.emit(m.printer.Note(note))
		m = shown.(model)
		if !m.running && *m.session.Config.Peers.Wake {
			woken, wake := m.reactToJobs()
			m = woken.(model)
			done = tea.Batch(done, wake)
		}
	}
	asked, ask := m.askNextPeer()
	return asked, tea.Batch(done, ask)
}

// peerSent shows a message this agent sent.
func (m model) peerSent(msg peerSentMsg) (tea.Model, tea.Cmd) {
	return m.emit(m.printer.PeerOut(msg.to, msg.text))
}

// peersCommand is /peers: who there is, and what this session changes of
// the settings.
func (m model) peersCommand(argument string) (tea.Model, tea.Cmd) {
	if m.session.Peers == nil || m.peers == nil {
		return m.emit(m.printer.Note("peers work only in mo's window"))
	}
	if !*m.session.Config.Peers.Enabled {
		return m.emit(m.printer.Note("peers are off · /settings › Peers turns them on"))
	}
	action, value, _ := strings.Cut(strings.TrimSpace(argument), " ")
	value = strings.TrimSpace(value)
	p := m.peers
	switch action {
	case "":
		if len(p.pending) > 0 && p.postponed {
			p.postponed = false
			return m.askNextPeer()
		}
		return m, m.listPeers()
	case "name":
		if value == "" {
			p.name = nil
		} else {
			p.name = &value
		}
		return m, tea.Batch(m.renamePeer(), printText(m.printer.Note("name: "+orDefault(m.peerName(), "folder@branch")+" · for this session")))
	case "incoming", "outgoing":
		if !slices.Contains(config.PeerApprovals, value) {
			return m.emit(m.printer.Note("use " + strings.Join(config.PeerApprovals, ", ")))
		}
		if action == "incoming" {
			p.incoming = value
		} else {
			p.outgoing = value
			m.session.Peers.SetOutgoing(value)
		}
		return m.emit(m.printer.Note(action + " messages: " + value + " · for this session"))
	case "on", "off":
		p.off = action == "off"
		return m, tea.Batch(m.applyPeers(), printText(m.printer.Note("peers "+action+" · for this session")))
	}
	return m.emit(m.printer.Note("usage: " + peersUsage))
}

// listPeers says who this instance is and which others there are.
func (m model) listPeers() tea.Cmd {
	node := m.session.Peers
	if !m.peersOn() {
		return printText(m.printer.Note("peers are off for this session · /peers on"))
	}
	incoming, outgoing := m.incomingSetting(), orDefault(m.peers.outgoing, m.session.Config.Peers.Outgoing)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), peerListTimeout)
		defer cancel()
		found, err := node.List(ctx)
		self := node.Self()
		lines := []string{fmt.Sprintf("you are %s (id %s) · incoming %s · outgoing %s", self.Name, self.ID, incoming, outgoing)}
		if len(found) == 0 {
			lines = append(lines, "no other mo is running with peers on")
		}
		for _, peer := range found {
			state := "idle"
			if peer.Working {
				state = "working"
			}
			line := fmt.Sprintf("%s · %s · %s", peer.Name, state, peer.Dir)
			if peer.Title != "" {
				line += fmt.Sprintf(" · %q", peer.Title)
			}
			lines = append(lines, line)
		}
		return peersListedMsg{text: strings.Join(lines, "\n"), err: err}
	}
}

// peerSuggestions are what /peers takes after what was typed.
func (m model) peerSuggestions(partial string) []command {
	options := []command{
		{name: "peers name ", description: "a name for this session; auto from the title; nothing for folder@branch", more: true},
		{name: "peers on", description: "take and send messages again"},
		{name: "peers off", description: "take and send none, for this session"},
	}
	for _, way := range []string{"incoming", "outgoing"} {
		for _, approval := range config.PeerApprovals {
			options = append(options, command{name: "peers " + way + " " + approval, description: way + " messages: " + approvalDescription[approval]})
		}
	}
	var matches []command
	for _, option := range options {
		if strings.HasPrefix(option.name, "peers "+partial) {
			matches = append(matches, option)
		}
	}
	return matches
}

var approvalDescription = map[string]string{
	"tools": "as the permission mode says: ask and edits ask, auto judges, all lets through",
	"ask":   "ask you every time",
	"auto":  "the model judges it, and asks you when unsure",
	"allow": "never ask",
}
