package team

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | TM-01 | Happy    | Tasks run at once, each its own agent; the caller gets only their reports, the rest is in their log | P0 |
// | TM-02 | Happy    | Only agents above the deepest level have the agent tool                   | P0       |
// | TM-03 | Edge     | One place to work and two levels: a parent waiting for its own gives its place up, nothing hangs | P0 |
// | TM-04 | Error    | More subagents than a turn allows are refused                             | P1       |
// | TM-05 | Happy    | Started in the background, the ids come back at once and the report is told when it ends | P0 |
// | TM-06 | Happy    | Stopped, it says how far it got; sent a message, it goes on with what it did | P0 |
// | TM-07 | Security | An agent cannot look into another branch's subagents                       | P1       |
// | TM-08 | Happy    | Saved and restored, a subagent that was working is stopped and goes on when sent a message | P1 |
// | TM-09 | Happy    | A subagent's question says who asks, and it is asking meanwhile            | P1       |
// | TM-10 | Happy    | Sent a message while it works, it reads it before it reports              | P0       |
// | TM-11 | Happy    | Stopping a subagent stops those under it                                  | P0       |
// | TM-12 | Edge     | Removing takes an ended subagent and its own off the list; a working one is refused | P1 |
// | TM-13 | Edge     | Out of steps, a subagent stops and says so                                | P1       |
// | TM-14 | Happy    | A subagent waits for those it started in the background and hears them before it reports | P0 |
// | TM-15 | Error    | Unknown ids, nothing to wait for and empty names are handled             | P2       |
// | TM-16 | Happy    | Stopped while waited for, the report says it stopped, not that it works   | P0       |
// | TM-17 | Happy    | A subagent tells the main agent, which hears it once and is woken          | P0       |
// | TM-18 | Happy    | A subagent tells its parent subagent, which reads it at its next step      | P0       |
// | TM-19 | Happy    | Told by one of its own, a parent that ended, or was stopped, goes on to read it | P0  |
// | TM-20 | Happy    | A run call resumed goes on with the subagents it started, from where they were, none new | P0 |
// | TM-21 | Happy    | Those started in the background go on when everything stopped them, not when stopped on their own | P0 |
// | TM-22 | Happy    | Saved and restored, a resumed call still finds its subagents and goes on with them | P0 |
// | TM-23 | Happy    | A subagent is built with its type, kept with its provider when saved and restored; its permissions are its agent's | P0 |

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

// fakeModel answers each agent by its task, the first message it got.
type fakeModel struct {
	mu      sync.Mutex
	working atomic.Int32
	most    atomic.Int32
	release chan struct{}
}

func (f *fakeModel) Stream(ctx context.Context, req llm.Request, _ func(llm.Event)) (llm.Response, error) {
	now := f.working.Add(1)
	defer f.working.Add(-1)
	for {
		most := f.most.Load()
		if now <= most || f.most.CompareAndSwap(most, now) {
			break
		}
	}
	task := req.Messages[0].Text
	last := req.Messages[len(req.Messages)-1]
	reply := func(text string) (llm.Response, error) {
		return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Text: text}}, nil
	}
	call := func(args string) (llm.Response, error) {
		return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: fmt.Sprint("c", len(req.Messages)), Name: tools.AgentName, Args: json.RawMessage(args)},
		}}}, nil
	}
	switch {
	case strings.HasPrefix(task, "leaf "):
		time.Sleep(20 * time.Millisecond)
		return reply("report of " + strings.TrimPrefix(task, "leaf "))
	case task == "slow" && last.Role == llm.RoleUser && last.Text == "slow":
		select {
		case <-f.release:
		case <-ctx.Done():
			return llm.Response{}, ctx.Err()
		}
		return reply("slow is done")
	case task == "slow":
		return reply("went on after: " + last.Text)
	case task == "parent" && last.Role == llm.RoleUser:
		return call(`{"task":"leaf child","name":"child"}`)
	case task == "parent":
		return reply("parent got: " + last.Text)
	case task == "loop":
		return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: fmt.Sprint("l", len(req.Messages)), Name: "ls", Args: json.RawMessage(`{"path":"."}`)},
		}}}, nil
	case task == "bgparent" && last.Role == llm.RoleUser && last.Text == "bgparent":
		return call(`{"task":"leaf late","name":"late","background":true}`)
	case task == "bgparent" && strings.Contains(last.Text, "ended"):
		return reply("heard: " + last.Text)
	case task == "bgparent":
		return reply("done before my subagent")
	case task == "slowparent" && last.Role == llm.RoleUser:
		return call(`{"task":"slow","name":"deep"}`)
	case task == "slowparent":
		return reply("slowparent got: " + last.Text)
	case task == "teller" && last.Role == llm.RoleUser:
		return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "t", Name: tools.TellParentName, Args: json.RawMessage(`{"message":"found it early"}`)},
		}}}, nil
	case task == "teller":
		return reply("teller done")
	case task == "relay" && strings.Contains(last.Text, "tells you"):
		return reply("relay heard: " + last.Text)
	case task == "relay" && last.Role == llm.RoleUser:
		return call(`{"task":"teller","name":"teller"}`)
	case task == "relay":
		return reply("relay did not hear")
	case task == "p2" && strings.Contains(last.Text, "tells you"):
		return reply("p2 woke: " + last.Text)
	case task == "p2" && last.Role == llm.RoleUser:
		return call(`{"task":"c2","name":"c2"}`)
	case task == "p2":
		return reply("p2 done")
	case task == "c2" && last.Text == "please tell":
		return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "t2", Name: tools.TellParentName, Args: json.RawMessage(`{"message":"news for you"}`)},
		}}}, nil
	case task == "c2" && last.Role == llm.RoleTool:
		return reply("c2 told")
	case task == "c2":
		return reply("c2 done")
	case task == "ask":
		if last.Role == llm.RoleUser {
			return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "q", Name: tools.AskName, Args: json.RawMessage(`{"questions":[{"question":"which one?"}]}`)},
			}}}, nil
		}
		return reply("told: " + last.Text)
	}
	return reply("?")
}

func newTeam(t *testing.T, settings Settings) (*Team, *fakeModel) {
	t.Helper()
	model := &fakeModel{release: make(chan struct{})}
	team := New()
	t.Cleanup(team.Close)
	workDir := t.TempDir()
	team.Configure(settings, func(info Info, below tools.Subagents, parent tools.Parent) (*agent.Agent, error) {
		registry := tools.NewRegistry(tools.Env{WorkDir: workDir, Agents: below, Parent: parent}, tools.Options{MaxOutput: 10000})
		return &agent.Agent{Provider: model, Tools: registry, MaxSteps: 10}, nil
	})
	return team, model
}

func within(t *testing.T, what string, run func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s hangs", what)
	}
}

var wide = Settings{Depth: 2, MaxParallel: 3, MaxAgents: 20}

func TestTeam(t *testing.T) {
	ctx := context.Background()

	t.Run("TM-01 tasks run at once, each its own agent; the caller gets only their reports, the rest is in their log", func(t *testing.T) {
		team, model := newTeam(t, wide)

		reports, err := team.Main().Run(ctx, []tools.SubTask{{Task: "leaf a", Name: "a"}, {Task: "leaf b", Name: "b"}}, false)
		log, _ := team.Main().Log(1)

		if err != nil || !strings.Contains(reports, "report of a") || !strings.Contains(reports, "report of b") || model.most.Load() != 2 {
			t.Errorf("err %v at once %d reports:\n%s", err, model.most.Load(), reports)
		}
		if !strings.Contains(log, "› leaf a") || !strings.Contains(log, "report of a") || !strings.Contains(log, "done") {
			t.Errorf("log:\n%s", log)
		}
	})

	t.Run("TM-02 only agents above the deepest level have the agent tool", func(t *testing.T) {
		team, _ := newTeam(t, Settings{Depth: 1, MaxParallel: 3, MaxAgents: 20})

		reports, err := team.Main().Run(ctx, []tools.SubTask{{Task: "parent", Name: "p"}}, false)

		if err != nil || !strings.Contains(reports, `unknown tool \"agent\"`) && !strings.Contains(reports, "unknown tool") {
			t.Errorf("err %v reports:\n%s", err, reports)
		}
	})

	t.Run("TM-03 one place to work and two levels: a parent waiting for its own gives its place up, nothing hangs", func(t *testing.T) {
		team, model := newTeam(t, Settings{Depth: 2, MaxParallel: 1, MaxAgents: 20})
		var reports string
		var err error

		within(t, "the tree", func() {
			reports, err = team.Main().Run(ctx, []tools.SubTask{{Task: "parent", Name: "p"}}, false)
		})

		if err != nil || !strings.Contains(reports, "parent got:") || !strings.Contains(reports, "report of child") || model.most.Load() != 1 {
			t.Errorf("err %v at once %d reports:\n%s", err, model.most.Load(), reports)
		}
		if infos := team.List(); len(infos) != 2 || infos[1].Parent != infos[0].ID || infos[1].Depth != 2 {
			t.Errorf("tree %+v", infos)
		}
	})

	t.Run("TM-04 more subagents than a turn allows are refused", func(t *testing.T) {
		team, _ := newTeam(t, Settings{Depth: 1, MaxParallel: 3, MaxAgents: 2})

		_, err := team.Main().Run(ctx, []tools.SubTask{{Task: "leaf a"}, {Task: "leaf b"}, {Task: "leaf c"}}, false)
		team.BeginTurn()
		_, again := team.Main().Run(ctx, []tools.SubTask{{Task: "leaf a"}}, false)

		if err == nil || again != nil {
			t.Errorf("err %v again %v", err, again)
		}
	})

	t.Run("TM-05 started in the background, the ids come back at once and the report is told when it ends", func(t *testing.T) {
		team, model := newTeam(t, wide)
		ended := make(chan struct{}, 1)
		team.Follow(nil, nil, func() { ended <- struct{}{} })

		started, err := team.Main().Run(ctx, []tools.SubTask{{Task: "slow", Name: "bg"}}, true)
		before := team.Main().HasEnded()
		close(model.release)
		within(t, "the end", func() { <-ended })
		notes := team.Main().TakeEnded()

		if err != nil || !strings.Contains(started, "agent 1 (bg)") || before {
			t.Errorf("err %v before %v started %q", err, before, started)
		}
		if len(notes) != 1 || !strings.Contains(notes[0], "slow is done") || team.Main().HasEnded() {
			t.Errorf("notes %q", notes)
		}
	})

	t.Run("TM-06 stopped, it says how far it got; sent a message, it goes on with what it did", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		started, _ := team.Main().Run(ctx, []tools.SubTask{{Task: "slow", Name: "s"}}, true)
		for team.List()[0].State != Running {
			time.Sleep(5 * time.Millisecond)
		}

		team.Stop(1)
		var report string
		within(t, "the stop", func() { report, _ = team.Main().Wait(ctx, []int{1}) })
		goneOn, err := team.Main().Send(ctx, 1, "go on please", false)

		if !strings.Contains(started, "agent 1") || !strings.Contains(report, "stopped") || !strings.Contains(report, "agent log 1") {
			t.Errorf("report:\n%s", report)
		}
		if err != nil || !strings.Contains(goneOn, "went on after: go on please") {
			t.Errorf("err %v went on:\n%s", err, goneOn)
		}
		if history := team.History(1); history[0].Text != "slow" {
			t.Errorf("history starts %+v", history[0])
		}
	})

	t.Run("TM-07 an agent cannot look into another branch's subagents", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "leaf a"}, {Task: "leaf b"}}, false)
		sibling := caller{team: team, id: 1}

		_, logErr := sibling.Log(2)
		_, stopErr := sibling.Stop(2)
		status := sibling.Status()

		if logErr == nil || stopErr == nil || status != "no subagents" {
			t.Errorf("log %v stop %v status %q", logErr, stopErr, status)
		}
	})

	t.Run("TM-08 saved and restored, a subagent that was working is stopped and goes on when sent a message", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "slow", Name: "s"}}, true)
		for team.List()[0].State != Running {
			time.Sleep(5 * time.Millisecond)
		}
		records := team.Records()

		restored, _ := newTeam(t, wide)
		restored.Restore(records)
		state := restored.List()[0].State
		goneOn, err := restored.Main().Send(ctx, 1, "again", false)

		if state != Stopped || err != nil || !strings.Contains(goneOn, "went on after: again") {
			t.Errorf("state %s err %v went on:\n%s", state, err, goneOn)
		}
	})

	t.Run("TM-09 a subagent's question says who asks, and it is asking meanwhile", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		var from string
		var stateWhileAsked State
		team.Follow(func(Info) agent.Hooks {
			return agent.Hooks{Interact: func(_ context.Context, interaction agent.Interaction) agent.Reply {
				from = interaction.From
				stateWhileAsked = team.List()[0].State
				return agent.Reply{Answers: []string{"the blue one"}}
			}}
		}, nil, nil)

		reports, err := team.Main().Run(ctx, []tools.SubTask{{Task: "ask", Name: "asker"}}, false)

		if err != nil || from != "asker (agent 1, level 1)" || stateWhileAsked != Asking || !strings.Contains(reports, "the blue one") {
			t.Errorf("err %v from %q state %s reports:\n%s", err, from, stateWhileAsked, reports)
		}
	})

	t.Run("TM-10 sent a message while it works, it reads it before it reports", func(t *testing.T) {
		team, model := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "slow", Name: "s"}}, true)
		waitState(t, team, 1, Running)

		queued, err := team.Main().Send(ctx, 1, "one more thing", false)
		close(model.release)
		var report string
		within(t, "the report", func() { report, _ = team.Main().Wait(ctx, []int{1}) })

		if err != nil || !strings.Contains(queued, "queued for agent 1") || !strings.Contains(report, "went on after: one more thing") {
			t.Errorf("err %v queued %q report:\n%s", err, queued, report)
		}
	})

	t.Run("TM-11 stopping a subagent stops those under it", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "slowparent", Name: "p"}}, true)
		waitState(t, team, 2, Running)

		_, err := team.Main().Stop(1)
		within(t, "the stop", func() { _, _ = team.Main().Wait(ctx, []int{1, 2}) })
		infos := team.List()

		if err != nil || infos[0].State != Stopped || infos[1].State != Stopped || team.Working() {
			t.Errorf("err %v tree %+v", err, infos)
		}
	})

	t.Run("TM-12 removing takes an ended subagent and its own off the list; a working one is refused", func(t *testing.T) {
		team, model := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "parent", Name: "p"}}, false)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "slow", Name: "s"}}, true)
		waitState(t, team, 3, Running)

		refused := team.Remove(3)
		err := team.Remove(1)
		left := team.List()
		close(model.release)

		if refused == nil || err != nil || len(left) != 1 || left[0].ID != 3 {
			t.Errorf("refused %v err %v left %+v", refused, err, left)
		}
	})

	t.Run("TM-13 out of steps, a subagent stops and says so", func(t *testing.T) {
		team, _ := newTeam(t, wide)

		report, err := team.Main().Run(ctx, []tools.SubTask{{Task: "loop", Name: "busy"}}, false)

		if err != nil || !strings.Contains(report, "step limit") || !strings.Contains(report, "agent send 1") {
			t.Errorf("err %v report:\n%s", err, report)
		}
	})

	t.Run("TM-14 a subagent waits for those it started in the background and hears them before it reports", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		var report string
		var err error

		within(t, "the tree", func() {
			report, err = team.Main().Run(ctx, []tools.SubTask{{Task: "bgparent", Name: "bg"}}, false)
		})

		if err != nil || !strings.Contains(report, "heard:") || !strings.Contains(report, "report of late") {
			t.Errorf("err %v report:\n%s", err, report)
		}
	})

	t.Run("TM-15 unknown ids, nothing to wait for and empty names are handled", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "leaf a"}}, false)

		_, logErr := team.Main().Log(9)
		_, sendErr := team.Main().Send(ctx, 9, "hi", false)
		nothing, waitErr := team.Main().Wait(ctx, nil)

		if logErr == nil || sendErr == nil || waitErr != nil || !strings.Contains(nothing, "no subagent") || team.List()[0].Name != "agent-1" {
			t.Errorf("log %v send %v wait %v %q list %+v", logErr, sendErr, waitErr, nothing, team.List())
		}
	})

	t.Run("TM-16 stopped while waited for, the report says it stopped, not that it works", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		stopping, stop := context.WithCancel(ctx)
		go func() {
			waitState(t, team, 1, Running)
			stop()
		}()

		report, err := team.Main().Run(stopping, []tools.SubTask{{Task: "slow", Name: "s"}}, false)

		if err == nil || !strings.Contains(report, "· stopped") || strings.Contains(report, "running") {
			t.Errorf("err %v report:\n%s", err, report)
		}
	})

	t.Run("TM-17 a subagent tells the main agent, which hears it once and is woken", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		woken := 0
		team.Follow(nil, nil, func() { woken++ })

		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "teller", Name: "teller"}}, false)
		has := team.Main().HasEnded()
		notes := team.Main().TakeEnded()

		if !has || len(notes) != 1 || !strings.Contains(notes[0], "teller (agent 1, level 1) tells you: found it early") || woken != 1 || team.Main().HasEnded() {
			t.Errorf("has %v woken %d notes %q", has, woken, notes)
		}
	})

	t.Run("TM-18 a subagent tells its parent subagent, which reads it at its next step", func(t *testing.T) {
		team, _ := newTeam(t, wide)

		report, err := team.Main().Run(ctx, []tools.SubTask{{Task: "relay", Name: "relay"}}, false)

		if err != nil || !strings.Contains(report, "relay heard:") || !strings.Contains(report, "found it early") || team.Main().HasEnded() {
			t.Errorf("err %v report:\n%s", err, report)
		}
	})

	t.Run("TM-19 told by one of its own, a parent that ended, or was stopped, goes on to read it", func(t *testing.T) {
		for _, stopped := range []bool{false, true} {
			team, _ := newTeam(t, wide)
			_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "p2", Name: "p2"}}, false)
			if stopped {
				records := team.Records()
				records[0].State = string(Stopped)
				team.Restore(records)
			}

			said, err := team.Send(2, "please tell")
			waitState(t, team, 2, Done)
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(lastText(team.History(1)), "p2 woke") && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			waitState(t, team, 1, Done)

			if err != nil || said != "it goes on" || !strings.Contains(lastText(team.History(1)), "news for you") || team.Main().HasEnded() {
				t.Errorf("stopped %v: err %v said %q parent ended with %q", stopped, err, said, lastText(team.History(1)))
			}
		}
	})

	t.Run("TM-20 a run call resumed goes on with the subagents it started, from where they were, none new", func(t *testing.T) {
		team, model := newTeam(t, wide)
		registry := tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Agents: team.Main()}, tools.Options{})
		call := llm.ToolCall{ID: "call-7", Name: tools.AgentName, Args: json.RawMessage(`{"task":"slow","name":"s"}`)}
		stopping, stop := context.WithCancel(ctx)
		go func() {
			waitState(t, team, 1, Running)
			team.StopAll()
			stop()
		}()

		_, _ = registry.Run(stopping, call)
		waitState(t, team, 1, Stopped)
		close(model.release)
		var report string
		within(t, "the resumed call", func() { report, _ = registry.Run(tools.WithResume(ctx), call) })

		if !strings.Contains(report, "slow is done") || len(team.List()) != 1 {
			t.Errorf("agents %+v report:\n%s", team.List(), report)
		}
	})

	t.Run("TM-21 those started in the background go on when everything stopped them, not when stopped on their own", func(t *testing.T) {
		team, model := newTeam(t, wide)
		_, _ = team.Main().Run(ctx, []tools.SubTask{{Task: "slow", Name: "all"}, {Task: "slow", Name: "own"}}, true)
		waitState(t, team, 1, Running)
		waitState(t, team, 2, Running)
		team.Stop(2)
		waitState(t, team, 2, Stopped)
		team.StopAll()
		waitState(t, team, 1, Stopped)

		close(model.release)
		team.GoOn(0)
		waitState(t, team, 1, Done)

		if info := team.List()[1]; info.State != Stopped {
			t.Errorf("the one stopped on its own went on: %+v", info)
		}
	})

	t.Run("TM-22 saved and restored, a resumed call still finds its subagents and goes on with them", func(t *testing.T) {
		team, _ := newTeam(t, wide)
		registry := tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Agents: team.Main()}, tools.Options{})
		call := llm.ToolCall{ID: "call-9", Name: tools.AgentName, Args: json.RawMessage(`{"task":"slow","name":"s"}`)}
		stopping, stop := context.WithCancel(ctx)
		go func() {
			waitState(t, team, 1, Running)
			stop()
		}()
		_, _ = registry.Run(stopping, call)
		waitState(t, team, 1, Stopped)
		records := team.Records()

		restored, model := newTeam(t, wide)
		restored.Restore(records)
		close(model.release)
		again := tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Agents: restored.Main()}, tools.Options{})
		var report string
		within(t, "the resumed call", func() { report, _ = again.Run(tools.WithResume(ctx), call) })

		if records[0].StartedBy != "call-9" || len(restored.List()) != 1 || !strings.Contains(report, "## agent 1 · s · done") {
			t.Errorf("started by %q agents %d report:\n%s", records[0].StartedBy, len(restored.List()), report)
		}
	})
}

// waitState waits for subagent id to be in a state.
func waitState(t *testing.T, team *Team, id int, state State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, info := range team.List() {
			if info.ID == id && info.State == state {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("agent %d never %s: %+v", id, state, team.List())
}

func TestTypes(t *testing.T) {
	t.Run("TM-23 a subagent is built with its type, kept with its provider when saved and restored; its permissions are its agent's", func(t *testing.T) {
		model := &fakeModel{release: make(chan struct{})}
		policy, _ := permissions.New(permissions.ModeAsk, nil, nil)
		var built []string
		var mu sync.Mutex
		configure := func(team *Team) {
			team.Configure(wide, func(info Info, below tools.Subagents, parent tools.Parent) (*agent.Agent, error) {
				mu.Lock()
				built = append(built, info.Type)
				mu.Unlock()
				registry := tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Agents: below, Parent: parent}, tools.Options{MaxOutput: 10000})
				return &agent.Agent{Provider: model, ProviderName: "lab", Model: "m1", Tools: registry, MaxSteps: 10, Permissions: policy}, nil
			})
		}
		team := New()
		t.Cleanup(team.Close)
		configure(team)

		_, err := team.Main().Run(context.Background(), []tools.SubTask{{Task: "leaf a", Name: "a", Type: "reviewer"}}, false)
		records := team.Records()
		restored := New()
		t.Cleanup(restored.Close)
		configure(restored)
		restored.Restore(records)
		_, sendErr := restored.Main().Send(context.Background(), 1, "again", false)

		info := restored.List()[0]
		if err != nil || sendErr != nil || records[0].Type != "reviewer" || records[0].Provider != "lab" || info.Type != "reviewer" || info.Provider != "lab" {
			t.Errorf("err %v %v record %+v info %+v", err, sendErr, records[0], info)
		}
		if restored.Permissions(1) != policy || restored.Permissions(99) != nil {
			t.Error("the permissions are not the subagent's")
		}
		if len(built) != 2 || built[0] != "reviewer" || built[1] != "reviewer" {
			t.Errorf("built %v", built)
		}
	})
}
