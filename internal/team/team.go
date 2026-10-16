// Package team runs the subagents the model hands tasks to: each an agent
// with its own conversation, in a tree under the main agent, as deep as the
// settings let it grow. What each did is kept, for its parents to read on
// demand and for the conversation to save.
package team

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
	"github.com/jg/mo/internal/tools"
)

// State is how a subagent is doing.
type State string

const (
	// Queued waits for a free place to start working.
	Queued State = "queued"
	// Running works: asks the model or runs a tool.
	Running State = "running"
	// Waiting waits for subagents of its own, without holding a place.
	Waiting State = "waiting"
	// Asking waits for the user to answer it.
	Asking  State = "asking"
	Done    State = "done"
	Failed  State = "failed"
	Stopped State = "stopped"
)

// Ended tells a state the subagent is no longer working in.
func (s State) Ended() bool {
	return s == Done || s == Failed || s == Stopped
}

// Settings are the limits of the tree.
type Settings struct {
	// Depth is how many levels of subagents there may be.
	Depth int
	// MaxParallel is how many work at once.
	MaxParallel int
	// MaxAgents is how many may start in one of the user's turns.
	MaxAgents int
}

// Info is what is known of a subagent, to show.
type Info struct {
	ID     int
	Parent int
	Depth  int
	Name   string
	Task   string
	State  State
	Err    string
	Steps  int
	Usage  llm.Usage
	// AskingBelow counts the subagents under it that wait for the user.
	AskingBelow int
	// Model and Window are what it runs on, once it was made.
	Model  string
	Window int
}

// Label is how a subagent is named to the user.
func (i Info) Label() string {
	return fmt.Sprintf("%s (agent %d, level %d)", i.Name, i.ID, i.Depth)
}

// Builder makes the agent of a subagent, with an empty conversation; agents
// are those it may start in turn, nil when it is at the deepest level.
type Builder func(info Info, agents tools.Subagents) (*agent.Agent, error)

// Team holds the subagents of a conversation.
type Team struct {
	mu       sync.Mutex
	members  []*member
	settings Settings
	build    Builder
	hooks    func(Info) agent.Hooks
	onChange func()
	onEnd    func()

	// active counts those working; wake is closed and made again each time
	// one stops, for those waiting for a place.
	active int
	wake   chan struct{}
	// started counts those started in the user's turn.
	started int

	root    context.Context
	stopAll context.CancelFunc
}

type member struct {
	Info
	agent *agent.Agent
	// history is what the agent did, as last seen from its own goroutine:
	// read from elsewhere, the agent's own would race.
	history []llm.Message
	result  string
	// live is the reply it is writing now, shown before it is whole.
	live     string
	liveSent time.Time
	cancel   context.CancelFunc
	// done is closed when the run in progress ends.
	done chan struct{}
	// background runs are told to the parent when they end; untold is
	// set until they are.
	background bool
	untold     bool
}

func New() *Team {
	root, stop := context.WithCancel(context.Background())
	return &Team{wake: make(chan struct{}), root: root, stopAll: stop}
}

// Configure sets the limits and how subagents are made, as the settings
// say; those running go on as they were made.
func (t *Team) Configure(settings Settings, build Builder) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.settings, t.build = settings, build
	t.signal()
}

// Follow sets what the user sees of the subagents: the hooks of each run,
// a call whenever one changes, and one when one started in the background
// ends.
func (t *Team) Follow(hooks func(Info) agent.Hooks, onChange, onEnd func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hooks, t.onChange, t.onEnd = hooks, onChange, onEnd
}

// BeginTurn starts counting the subagents of a new turn of the user's.
func (t *Team) BeginTurn() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = 0
}

// Main is what the main agent may start and look into: every subagent.
func (t *Team) Main() tools.Subagents {
	return caller{team: t, id: 0}
}

// List is every subagent, parents before their own.
func (t *Team) List() []Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	var infos []Info
	var walk func(parent int)
	walk = func(parent int) {
		for _, m := range t.members {
			if m.Parent == parent {
				info := m.Info
				info.AskingBelow = t.askingBelow(m.ID)
				infos = append(infos, info)
				walk(m.ID)
			}
		}
	}
	walk(0)
	return infos
}

// History is what a subagent did, as last seen.
func (t *Team) History(id int) []llm.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	if m := t.find(id); m != nil {
		return slices.Clone(m.history)
	}
	return nil
}

// Live is the reply a subagent is writing now.
func (t *Team) Live(id int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if m := t.find(id); m != nil {
		return m.live
	}
	return ""
}

// Path is the names from the main agent's first subagent down to id.
func (t *Team) Path(id int) []Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	var path []Info
	for m := t.find(id); m != nil; m = t.find(m.Parent) {
		path = append([]Info{m.Info}, path...)
	}
	return path
}

// Send is a message of the user's to a subagent: queued while it works,
// else it goes on from where it was. Its parent is not told: it is the
// user's talk with it.
func (t *Team) Send(id int, message string) (string, error) {
	t.mu.Lock()
	m := t.find(id)
	if m == nil {
		t.mu.Unlock()
		return "", fmt.Errorf("no subagent %d", id)
	}
	if !m.State.Ended() {
		m.agent.Queue(message)
		t.mu.Unlock()
		return "queued: it reads it at its next step", nil
	}
	if err := t.ensureAgent(m); err != nil {
		t.mu.Unlock()
		return "", err
	}
	t.mu.Unlock()
	t.start(t.root, m, message, false)
	return "it goes on", nil
}

// Stop stops a subagent and every one under it.
func (t *Team) Stop(id int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopTree(id)
}

// StopAll stops every subagent, as esc does with the main agent.
func (t *Team) StopAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopAllLocked()
}

// Working tells whether a subagent is working, or waits to.
func (t *Team) Working() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range t.members {
		if !m.State.Ended() {
			return true
		}
	}
	return false
}

// Remove takes an ended subagent and those under it off the list.
func (t *Team) Remove(id int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	gone := map[int]bool{id: true}
	for _, m := range t.members {
		if gone[m.Parent] {
			gone[m.ID] = true
		}
	}
	for _, m := range t.members {
		if gone[m.ID] && !m.State.Ended() {
			return fmt.Errorf("%s is still working: stop it first", m.Label())
		}
	}
	t.members = slices.DeleteFunc(t.members, func(m *member) bool { return gone[m.ID] })
	t.changed()
	return nil
}

// Close stops every subagent for good.
func (t *Team) Close() {
	t.stopAll()
}

// Records are the subagents to save with the conversation.
func (t *Team) Records() []sessions.AgentRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	var records []sessions.AgentRecord
	for _, m := range t.members {
		records = append(records, sessions.AgentRecord{
			ID: m.ID, Parent: m.Parent, Depth: m.Depth, Name: m.Name, Task: m.Task, State: string(m.State),
			Error: m.Err, Result: m.result, History: slices.Clone(m.history), Usage: m.Usage,
		})
	}
	return records
}

// Restore puts back the subagents of a saved conversation, in place of
// any there were: ended, those that were working stopped, each ready to go
// on when sent a message.
func (t *Team) Restore(records []sessions.AgentRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopAllLocked()
	t.members = nil
	for _, record := range records {
		state := State(record.State)
		if !state.Ended() {
			state = Stopped
		}
		t.members = append(t.members, &member{
			Info: Info{ID: record.ID, Parent: record.Parent, Depth: record.Depth, Name: record.Name, Task: record.Task,
				State: state, Err: record.Error, Steps: steps(record.History), Usage: record.Usage},
			history: record.History, result: record.Result,
		})
	}
	t.changed()
}

// stopAllLocked stops every subagent; the lock is held.
func (t *Team) stopAllLocked() {
	for _, m := range t.members {
		if m.cancel != nil {
			m.cancel()
		}
	}
}

func (t *Team) find(id int) *member {
	for _, m := range t.members {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// under tells whether id is ancestor's, or below one of its own.
func (t *Team) under(id, ancestor int) bool {
	for m := t.find(id); m != nil; m = t.find(m.Parent) {
		if m.Parent == ancestor {
			return true
		}
	}
	return false
}

func (t *Team) askingBelow(id int) int {
	count := 0
	for _, m := range t.members {
		if m.State == Asking && t.under(m.ID, id) {
			count++
		}
	}
	return count
}

func (t *Team) stopTree(id int) {
	for _, m := range t.members {
		if (m.ID == id || t.under(m.ID, id)) && m.cancel != nil {
			m.cancel()
		}
	}
}

// changed tells the user something changed; the lock is held, so the call
// goes on its own.
func (t *Team) changed() {
	if t.onChange != nil {
		go t.onChange()
	}
}

func (t *Team) signal() {
	close(t.wake)
	t.wake = make(chan struct{})
}

// acquire waits for a place to work.
func (t *Team) acquire(ctx context.Context) error {
	for {
		t.mu.Lock()
		if t.active < max(t.settings.MaxParallel, 1) {
			t.active++
			t.mu.Unlock()
			return nil
		}
		wake := t.wake
		t.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *Team) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active--
	t.signal()
}

func (t *Team) setState(m *member, state State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m.State = state
	t.changed()
}

// snapshot keeps what the agent did so far; called from its goroutine.
func (t *Team) snapshot(m *member) {
	history := slices.Clone(m.agent.History)
	t.mu.Lock()
	defer t.mu.Unlock()
	m.history = history
	m.Steps = steps(history)
}

func steps(history []llm.Message) int {
	count := 0
	for _, message := range history {
		if message.Role == llm.RoleAssistant {
			count++
		}
	}
	return count
}

// start runs a subagent on a message, in the background of the call that
// started it: ctx stops it, and so does stopping it or everything.
func (t *Team) start(ctx context.Context, m *member, message string, background bool) {
	runCtx, cancel := context.WithCancel(ctx)
	stopWithAll := context.AfterFunc(t.root, cancel)
	t.mu.Lock()
	m.State, m.Err, m.cancel, m.background, m.untold = Queued, "", cancel, background, false
	// What it was asked shows and is saved before its agent takes it in.
	m.history = append(slices.Clone(m.history), llm.Message{Role: llm.RoleUser, Text: message})
	m.done = make(chan struct{})
	done := m.done
	hooks := t.memberHooks(m)
	t.changed()
	t.mu.Unlock()
	go func() {
		defer close(done)
		defer stopWithAll()
		defer cancel()
		err := t.acquire(runCtx)
		if err == nil {
			t.setState(m, Running)
			err = m.agent.Run(runCtx, message, hooks)
			for err == nil {
				// Before it reports: what it was sent during its last step,
				// and the subagents it left working in the background.
				if m.agent.HasQueued() {
					err = m.agent.RunQueued(runCtx, hooks)
					continue
				}
				if err = t.waitFor(runCtx, m.ID, t.backgroundOf(m.ID)); err == nil && m.agent.HasEndedJobs() {
					err = m.agent.RunEndedJobs(runCtx, hooks)
					continue
				}
				break
			}
			t.release()
		}
		t.snapshot(m)
		t.finish(m, err)
	}()
}

func (t *Team) finish(m *member, err error) {
	t.mu.Lock()
	m.result = lastText(m.history)
	switch {
	case err == nil:
		m.State = Done
	case errors.Is(err, context.Canceled):
		m.State, m.Err = Stopped, "stopped"
	case errors.Is(err, agent.ErrStepLimit):
		m.State, m.Err = Stopped, err.Error()
	default:
		m.State, m.Err = Failed, err.Error()
	}
	m.cancel, m.live = nil, ""
	m.untold = m.background
	onEnd := t.onEnd
	toMain := m.background && m.Parent == 0
	t.changed()
	t.mu.Unlock()
	if toMain && onEnd != nil {
		onEnd()
	}
}

// memberHooks are the user's hooks for a subagent's run, which keep what
// it did and say who asks; the lock is held.
func (t *Team) memberHooks(m *member) agent.Hooks {
	var hooks agent.Hooks
	if t.hooks != nil {
		hooks = t.hooks(m.Info)
	}
	interact, onUsage, onToolDone, onEvent, onAssistant := hooks.Interact, hooks.OnUsage, hooks.OnToolDone, hooks.OnEvent, hooks.OnAssistant
	hooks.OnEvent = func(event llm.Event) {
		if event.Kind == llm.EventText {
			t.mu.Lock()
			m.live += event.Text
			// The reply as it comes, a few times a second.
			if time.Since(m.liveSent) > liveEvery {
				m.liveSent = time.Now()
				t.changed()
			}
			t.mu.Unlock()
		}
		if onEvent != nil {
			onEvent(event)
		}
	}
	hooks.OnAssistant = func(message llm.Message) {
		t.mu.Lock()
		m.live = ""
		t.mu.Unlock()
		if onAssistant != nil {
			onAssistant(message)
		}
	}
	hooks.Interact = nil
	if interact != nil {
		hooks.Interact = func(ctx context.Context, interaction agent.Interaction) agent.Reply {
			interaction.From = m.Label()
			t.setState(m, Asking)
			defer t.setState(m, Running)
			return interact(ctx, interaction)
		}
	}
	hooks.OnUsage = func(usage llm.Usage) {
		t.snapshot(m)
		t.mu.Lock()
		m.Usage = usage
		t.changed()
		t.mu.Unlock()
		if onUsage != nil {
			onUsage(usage)
		}
	}
	hooks.OnToolDone = func(call llm.ToolCall, output string, isError bool) {
		t.snapshot(m)
		if onToolDone != nil {
			onToolDone(call, output, isError)
		}
	}
	return hooks
}

// lastText is the last thing the agent wrote: its report.
func lastText(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleAssistant && strings.TrimSpace(history[i].Text) != "" {
			return strings.TrimSpace(history[i].Text)
		}
	}
	return ""
}

// report is how a subagent ended, for the one that waited for it.
func (t *Team) report(m *member) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	head := fmt.Sprintf("## agent %d · %s · %s", m.ID, m.Name, m.State)
	switch m.State {
	case Done:
		return head + "\n" + orNothing(m.result)
	case Failed:
		return fmt.Sprintf("%s: %s\nagent log %d shows how far it got; agent send %d with a message lets it go on.", head, m.Err, m.ID, m.ID)
	}
	why := ""
	if m.Err != "" && m.Err != string(Stopped) {
		why = ": " + m.Err
	}
	text := fmt.Sprintf("%s%s after %d steps\nagent log %d shows how far it got; agent send %d with a message lets it go on.", head, why, m.Steps, m.ID, m.ID)
	if m.result != "" {
		text += "\nIt last wrote: " + m.result
	}
	return text
}

func orNothing(text string) string {
	if text == "" {
		return "(it wrote no report)"
	}
	return text
}

// ensureAgent makes a restored subagent's agent, with what it did.
func (t *Team) ensureAgent(m *member) error {
	if m.agent != nil {
		return nil
	}
	agent, err := t.makeAgent(m.Info)
	if err != nil {
		return err
	}
	agent.History = slices.Clone(m.history)
	m.agent = agent
	m.Model, m.Window = agent.Model, agent.Window
	return nil
}

// makeAgent makes a subagent's agent, with the agent tool when it is not
// at the deepest level; the lock is held.
func (t *Team) makeAgent(info Info) (*agent.Agent, error) {
	if t.build == nil {
		return nil, errors.New("subagents are off")
	}
	var below tools.Subagents
	if info.Depth < t.settings.Depth {
		below = caller{team: t, id: info.ID}
	}
	made, err := t.build(info, below)
	if err != nil {
		return nil, err
	}
	if m := t.find(info.ID); m != nil {
		m.Model, m.Window = made.Model, made.Window
	}
	return made, nil
}

// liveEvery is how often the reply being written is shown again.
const liveEvery = 100 * time.Millisecond

// settleTime is how long a report waits for subagents being stopped to
// end, to say how they did.
const settleTime = 5 * time.Second

// settle waits a while for runs being stopped to end.
func (t *Team) settle(members []*member, within time.Duration) {
	t.mu.Lock()
	var dones []chan struct{}
	for _, m := range members {
		if m.done != nil {
			dones = append(dones, m.done)
		}
	}
	t.mu.Unlock()
	deadline := time.After(within)
	for _, done := range dones {
		select {
		case <-done:
		case <-deadline:
			return
		}
	}
}

// backgroundOf are the subagents id started in the background that still
// work.
func (t *Team) backgroundOf(id int) []*member {
	t.mu.Lock()
	defer t.mu.Unlock()
	var working []*member
	for _, m := range t.members {
		if m.Parent == id && m.background && !m.State.Ended() {
			working = append(working, m)
		}
	}
	return working
}

// waitFor waits for runs to end. A subagent waiting for its own gives up
// its place meanwhile, so they can work, and takes one again after.
func (t *Team) waitFor(ctx context.Context, waiter int, members []*member) error {
	t.mu.Lock()
	self := t.find(waiter)
	var dones []chan struct{}
	for _, m := range members {
		if m.done != nil {
			dones = append(dones, m.done)
		}
	}
	t.mu.Unlock()
	if len(dones) == 0 {
		return nil
	}
	if self != nil {
		t.setState(self, Waiting)
		t.release()
		defer func() {
			// A place is taken again even when stopped: the run that
			// waits gives it back as it ends.
			for t.acquire(context.Background()) != nil {
			}
			t.setState(self, Running)
		}()
	}
	for _, done := range dones {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
