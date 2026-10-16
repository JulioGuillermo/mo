package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AV-01 | Happy    | The panel lists the subagents as a tree, open, each with its sign and state | P0     |
// | AV-02 | Happy    | Each state has its sign: clocks while waiting, yellow when one under it asks, ? asking, ✓ done, ⚠ failed or stopped | P0 |
// | AV-03 | Happy    | /agents n opens its view: a title bar with where it is and ✕, those under it beside, what it did | P0 |
// | AV-04 | Happy    | ctrl+x closes the view, also while something is asked; ✕ and main close it, a parent's name opens it | P0 |
// | AV-05 | Happy    | Esc in the view stops that subagent and not the main agent; enter sends it a message | P0 |
// | AV-06 | Happy    | A right click offers to stop one that works, or remove one that ended, which closes its view | P1 |
// | AV-07 | Happy    | What it is writing shows in its view as it comes                          | P1       |
// | AV-08 | Edge     | /agents without subagents says how to have them                           | P2       |

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
)

// holdingModel answers a task "hold" only once released, writing "partial"
// first; any other task, and anything after, at once.
type holdingModel struct {
	once    sync.Once
	release chan struct{}
}

func (h *holdingModel) Stream(ctx context.Context, req llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	last := req.Messages[len(req.Messages)-1]
	if req.Messages[0].Text == "hold" && last.Text == "hold" {
		onEvent(llm.Event{Kind: llm.EventText, Text: "partial words"})
		select {
		case <-h.release:
		case <-ctx.Done():
			return llm.Response{}, ctx.Err()
		}
	}
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: "answer to " + last.Text}}, nil
}

// teamModel is a conversation whose subagents run on a holding model.
func teamModel(t *testing.T) (model, *holdingModel) {
	t.Helper()
	m := conversationModel()
	m.width, m.height = 150, 40
	provider := &holdingModel{release: make(chan struct{})}
	subagents := team.New()
	t.Cleanup(subagents.Close)
	workDir := t.TempDir()
	subagents.Configure(team.Settings{Depth: 2, MaxParallel: 3, MaxAgents: 20}, func(info team.Info, below tools.Subagents) (*agent.Agent, error) {
		return &agent.Agent{Provider: provider, Tools: tools.NewRegistry(tools.Env{WorkDir: workDir, Agents: below}, tools.Options{}), Model: "fake-model", Window: 1000}, nil
	})
	m.session.Team = subagents
	return m, provider
}

// restoredTree is a lead with two of its own, one under the other.
func restoredTree() []sessions.AgentRecord {
	return []sessions.AgentRecord{
		{ID: 1, Depth: 1, Name: "lead", Task: "lead the way", State: "done", History: []llm.Message{
			{Role: llm.RoleUser, Text: "lead the way"}, {Role: llm.RoleAssistant, Text: "led it"},
		}},
		{ID: 2, Parent: 1, Depth: 2, Name: "scout", Task: "look", State: "failed", Error: "boom"},
		{ID: 3, Parent: 0, Depth: 1, Name: "other", Task: "other", State: "done"},
	}
}

func waitAgent(t *testing.T, m model, id int, state team.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := m.agentInfo(id); ok && info.State == state {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	info, _ := m.agentInfo(id)
	t.Fatalf("agent %d is %s, not %s", id, info.State, state)
}

func screenOf(m model) string {
	return ansi.Strip(m.View().Content)
}

func TestAgentView(t *testing.T) {
	t.Run("AV-01 the panel lists the subagents as a tree, open, each with its sign and state", func(t *testing.T) {
		m, _ := teamModel(t)
		m.session.Team.Restore(restoredTree())

		view := screenOf(m)

		for _, row := range []string{"Agents", "✓ lead · done", "  ⚠ scout · failed", "✓ other · done"} {
			if !strings.Contains(view, row) {
				t.Errorf("%q not in:\n%s", row, view)
			}
		}
	})

	t.Run("AV-02 each state has its sign", func(t *testing.T) {
		cases := []struct {
			info team.Info
			mark string
		}{
			{team.Info{State: team.Queued}, "⧗"}, {team.Info{State: team.Waiting}, "⧗"}, {team.Info{State: team.Running}, "●"},
			{team.Info{State: team.Asking}, "?"}, {team.Info{State: team.Done}, "✓"}, {team.Info{State: team.Failed}, "⚠"}, {team.Info{State: team.Stopped}, "⚠"},
		}
		for _, c := range cases {
			if mark, _ := agentMark(c.info, true); mark != c.mark {
				t.Errorf("%s: %q", c.info.State, mark)
			}
		}
		_, waiting := agentMark(team.Info{State: team.Waiting}, true)
		_, queued := agentMark(team.Info{State: team.Queued}, true)
		_, waitingOnAsk := agentMark(team.Info{State: team.Waiting, AskingBelow: 1}, true)
		_, asking := agentMark(team.Info{State: team.Asking}, true)
		if waiting == queued || waitingOnAsk != asking {
			t.Errorf("waiting %v queued %v waiting on an ask %v asking %v", waiting, queued, waitingOnAsk, asking)
		}
	})

	t.Run("AV-03 /agents n opens its view: a title bar with where it is and ✕, those under it beside, what it did", func(t *testing.T) {
		m, _ := teamModel(t)
		m.session.Team.Restore(restoredTree())

		updated, _ := m.runCommand("/agents 1")
		m = updated.(model)
		view := screenOf(m)
		lines := strings.Split(view, "\n")

		if m.agentView == nil || !strings.Contains(lines[0], "lead · level 1 · main › lead · done") || !strings.HasSuffix(strings.TrimRight(lines[0], " "), "✕") {
			t.Fatalf("title %q", lines[0])
		}
		for _, want := range []string{"Under it", "⚠ scout · failed", "lead the way", "led it", "message for lead"} {
			if !strings.Contains(view, want) {
				t.Errorf("%q not in:\n%s", want, view)
			}
		}
		if strings.Contains(view, "other · done") {
			t.Errorf("another branch shows:\n%s", view)
		}
	})

	t.Run("AV-04 ctrl+x closes the view, also while something is asked; ✕ and main close it, a parent's name opens it", func(t *testing.T) {
		m, _ := teamModel(t)
		m.session.Team.Restore(restoredTree())
		open := func(id int) model {
			updated, _ := m.openAgentView(id)
			return updated.(model)
		}
		click := func(m model, text string) model {
			t.Helper()
			title := strings.Split(screenOf(m), "\n")[0]
			x := strings.Index(title, text)
			if x < 0 {
				t.Fatalf("%q not in %q", text, title)
			}
			updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: ansi.StringWidth(title[:x]), Y: 0, Button: tea.MouseLeft}))
			return updated.(model)
		}

		asked := open(2)
		updated, _ := asked.Update(interactionMsg{interaction: agent.Interaction{Questions: []tools.Question{{Text: "which?"}}, From: "scout"}, reply: make(chan agent.Reply, 1)})
		asked = updated.(model)
		closedAsked := keys(t, asked, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		byCross := click(open(2), "✕")
		byMain := click(open(2), "main")
		toParent := click(open(2), "lead")

		if closedAsked.agentView != nil || closedAsked.dialog == nil {
			t.Errorf("ctrl+x with a question: view %v dialog %v", closedAsked.agentView != nil, closedAsked.dialog != nil)
		}
		if byCross.agentView != nil || byMain.agentView != nil || toParent.agentView == nil || toParent.agentView.id != 1 {
			t.Errorf("✕ %v main %v parent %+v", byCross.agentView != nil, byMain.agentView != nil, toParent.agentView)
		}
	})

	t.Run("AV-05 esc in the view stops that subagent and not the main agent; enter sends it a message", func(t *testing.T) {
		m, provider := teamModel(t)
		mainStopped := false
		m.running, m.cancel = true, func() { mainStopped = true }
		_, _ = m.session.Team.Main().Run(context.Background(), []tools.SubTask{{Task: "hold", Name: "h"}, {Task: "quick", Name: "q"}}, true)
		waitAgent(t, m, 1, team.Running)
		waitAgent(t, m, 2, team.Done)

		updated, _ := m.openAgentView(1)
		m = keys(t, updated.(model), keyEsc)
		waitAgent(t, m, 1, team.Stopped)
		close(provider.release)
		updated, _ = m.openAgentView(2)
		m = typeIn(t, updated.(model), "and more")
		m = keys(t, m, keyEnter)
		waitAgent(t, m, 2, team.Done)

		if mainStopped || !m.running {
			t.Error("esc in the view stopped the main agent")
		}
		if history := m.session.Team.History(2); len(history) != 4 || history[3].Text != "answer to and more" {
			t.Errorf("history %+v", history)
		}
	})

	t.Run("AV-06 a right click offers to stop one that works, or remove one that ended, which closes its view", func(t *testing.T) {
		m, provider := teamModel(t)
		defer close(provider.release)
		_, _ = m.session.Team.Main().Run(context.Background(), []tools.SubTask{{Task: "hold", Name: "h"}, {Task: "quick", Name: "q"}}, true)
		waitAgent(t, m, 1, team.Running)
		waitAgent(t, m, 2, team.Done)

		working, _ := m.agentMenu(1)
		ended, _ := m.agentMenu(2)
		opened, _ := m.openAgentView(2)
		removed, _ := opened.(model).agentMenu(2)
		gone, _ := removed.(model).popup.do(removed.(model), "remove")

		if !hasOption(working.(model).popup, "stop") || hasOption(working.(model).popup, "remove") || !hasOption(ended.(model).popup, "remove") {
			t.Errorf("working %+v ended %+v", working.(model).popup.options, ended.(model).popup.options)
		}
		if _, listed := gone.(model).agentInfo(2); listed || gone.(model).agentView != nil {
			t.Errorf("listed %v view %v", listed, gone.(model).agentView != nil)
		}
	})

	t.Run("AV-07 what it is writing shows in its view as it comes", func(t *testing.T) {
		m, provider := teamModel(t)
		defer close(provider.release)
		_, _ = m.session.Team.Main().Run(context.Background(), []tools.SubTask{{Task: "hold", Name: "h"}}, true)
		waitAgent(t, m, 1, team.Running)
		deadline := time.Now().Add(5 * time.Second)
		for m.session.Team.Live(1) == "" && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}

		updated, _ := m.openAgentView(1)

		if view := screenOf(updated.(model)); !strings.Contains(view, "partial words") || !strings.Contains(view, "● h · level 1") {
			t.Errorf("view:\n%s", view)
		}
	})

	t.Run("AV-08 /agents without subagents says how to have them", func(t *testing.T) {
		m := conversationModel()

		updated, _ := m.runCommand("/agents")

		if !strings.Contains(shown(updated.(model)), "agents.enabled") {
			t.Errorf("transcript:\n%s", shown(updated.(model)))
		}
	})
}

func hasOption(p *popup, value string) bool {
	if p == nil {
		return false
	}
	for _, option := range p.options {
		if option.Value == value {
			return true
		}
	}
	return false
}
