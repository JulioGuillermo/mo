package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// caller is the team as one agent sees it: id 0 is the main agent. It may
// start subagents of its own and look into those below it.
type caller struct {
	team *Team
	id   int
}

func (c caller) depth() int {
	if m := c.team.find(c.id); m != nil {
		return m.Depth
	}
	return 0
}

// mine finds a subagent below the caller.
func (c caller) mine(id int) (*member, error) {
	m := c.team.find(id)
	if m == nil || !c.team.under(id, c.id) {
		return nil, fmt.Errorf("no subagent %d of yours: agent status lists them", id)
	}
	return m, nil
}

func (c caller) Run(ctx context.Context, tasks []tools.SubTask, background bool) (string, error) {
	t := c.team
	t.mu.Lock()
	depth := c.depth() + 1
	switch {
	case depth > t.settings.Depth:
		t.mu.Unlock()
		return "", fmt.Errorf("no subagents at this depth: the settings allow %d levels", t.settings.Depth)
	case t.started+len(tasks) > t.settings.MaxAgents:
		t.mu.Unlock()
		return "", fmt.Errorf("%d subagents started in this turn already, of at most %d: do the rest yourself, or send to those there are", t.started, t.settings.MaxAgents)
	}
	var started []*member
	for _, task := range tasks {
		id := 1
		for _, m := range t.members {
			id = max(id, m.ID+1)
		}
		name := strings.TrimSpace(task.Name)
		if name == "" {
			name = fmt.Sprintf("agent-%d", id)
		}
		m := &member{Info: Info{ID: id, Parent: c.id, Depth: depth, Name: name, Task: task.Task}}
		agent, err := t.makeAgent(m.Info)
		if err != nil {
			t.mu.Unlock()
			return "", err
		}
		m.agent = agent
		m.Model, m.Window = agent.Model, agent.Window
		t.members = append(t.members, m)
		t.started++
		started = append(started, m)
	}
	t.mu.Unlock()
	base := ctx
	if background {
		base = t.root
	}
	for _, m := range started {
		t.start(base, m, m.Task, background)
	}
	if background {
		var names []string
		for _, m := range started {
			names = append(names, fmt.Sprintf("agent %d (%s)", m.ID, m.Name))
		}
		return "started in the background: " + strings.Join(names, ", ") + ". You hear when each ends; agent wait gives their reports sooner.", nil
	}
	return c.reports(ctx, started)
}

// reports waits for the runs and gives how each ended.
func (c caller) reports(ctx context.Context, members []*member) (string, error) {
	err := c.team.waitFor(ctx, c.id, members)
	if err != nil {
		// Stopped with them, they end in a moment: the reports say how.
		c.team.settle(members, settleTime)
	}
	var out []string
	for _, m := range members {
		c.team.mu.Lock()
		m.untold = false
		c.team.mu.Unlock()
		out = append(out, c.team.report(m))
	}
	return strings.Join(out, "\n\n"), err
}

func (c caller) Send(ctx context.Context, id int, message string, background bool) (string, error) {
	t := c.team
	t.mu.Lock()
	m, err := c.mine(id)
	if err != nil {
		t.mu.Unlock()
		return "", err
	}
	if !m.State.Ended() {
		m.agent.Queue(message)
		t.mu.Unlock()
		return fmt.Sprintf("queued for agent %d (%s): it reads it at its next step", m.ID, m.Name), nil
	}
	if err := t.ensureAgent(m); err != nil {
		t.mu.Unlock()
		return "", err
	}
	t.mu.Unlock()
	base := ctx
	if background {
		base = t.root
	}
	t.start(base, m, message, background)
	if background {
		return fmt.Sprintf("agent %d (%s) goes on in the background; you hear when it ends", m.ID, m.Name), nil
	}
	return c.reports(ctx, []*member{m})
}

func (c caller) Wait(ctx context.Context, ids []int) (string, error) {
	t := c.team
	t.mu.Lock()
	var members []*member
	if len(ids) == 0 {
		for _, m := range t.members {
			if m.Parent == c.id && m.background && (!m.State.Ended() || m.untold) {
				members = append(members, m)
			}
		}
	}
	for _, id := range ids {
		m, err := c.mine(id)
		if err != nil {
			t.mu.Unlock()
			return "", err
		}
		members = append(members, m)
	}
	t.mu.Unlock()
	if len(members) == 0 {
		return "no subagent of yours is working in the background", nil
	}
	return c.reports(ctx, members)
}

func (c caller) Log(id int) (string, error) {
	t := c.team
	t.mu.Lock()
	m, err := c.mine(id)
	if err != nil {
		t.mu.Unlock()
		return "", err
	}
	head := fmt.Sprintf("agent %d · %s · level %d · %s · %d steps\n", m.ID, m.Name, m.Depth, m.State, m.Steps)
	history := m.history
	t.mu.Unlock()
	return head + Transcript(history), nil
}

// Transcript is a subagent's conversation as it reads: its task and the
// messages it got, what it wrote, each call and its result.
func Transcript(history []llm.Message) string {
	var out strings.Builder
	for _, message := range history {
		switch message.Role {
		case llm.RoleUser:
			fmt.Fprintf(&out, "› %s\n", message.Text)
		case llm.RoleAssistant:
			if text := strings.TrimSpace(message.Text); text != "" {
				fmt.Fprintf(&out, "%s\n", text)
			}
			for _, call := range message.ToolCalls {
				fmt.Fprintf(&out, "→ %s %s\n", call.Name, compactJSON(call.Args))
			}
		case llm.RoleTool:
			mark := "←"
			if message.IsError {
				mark = "← error:"
			}
			fmt.Fprintf(&out, "%s %s\n", mark, strings.TrimRight(message.Text, "\n"))
		}
	}
	return out.String()
}

func compactJSON(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func (c caller) Status() string {
	t := c.team
	t.mu.Lock()
	defer t.mu.Unlock()
	var lines []string
	for _, m := range t.members {
		if !t.under(m.ID, c.id) {
			continue
		}
		line := fmt.Sprintf("%s%d %s · level %d · %s · %d steps · %s", strings.Repeat("  ", m.Depth-c.depth()-1), m.ID, m.Name, m.Depth, m.State, m.Steps, oneLine(m.Task, 80))
		if m.Err != "" {
			line += " · " + m.Err
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "no subagents"
	}
	return strings.Join(lines, "\n")
}

func oneLine(text string, width int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > width {
		return string([]rune(text)[:width-1]) + "…"
	}
	return text
}

func (c caller) Stop(id int) (string, error) {
	t := c.team
	t.mu.Lock()
	defer t.mu.Unlock()
	m, err := c.mine(id)
	if err != nil {
		return "", err
	}
	if m.State.Ended() {
		return fmt.Sprintf("agent %d (%s) is not working: it %s", m.ID, m.Name, m.State), nil
	}
	t.stopTree(id)
	return fmt.Sprintf("stopping agent %d (%s) and those under it", m.ID, m.Name), nil
}

func (c caller) TakeEnded() []string {
	t := c.team
	t.mu.Lock()
	told := t.told[c.id]
	delete(t.told, c.id)
	var ended []*member
	for _, m := range t.members {
		if m.Parent == c.id && m.untold && m.State.Ended() {
			m.untold = false
			ended = append(ended, m)
		}
	}
	t.mu.Unlock()
	notes := told
	for _, m := range ended {
		notes = append(notes, "subagent started in the background ended:\n"+t.report(m))
	}
	return notes
}

func (c caller) HasEnded() bool {
	t := c.team
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.told[c.id]) > 0 {
		return true
	}
	for _, m := range t.members {
		if m.Parent == c.id && m.untold && m.State.Ended() {
			return true
		}
	}
	return false
}
