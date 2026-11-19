package tui

// Test Traceability Table
// | ID    | Category | Description                                                                    | Priority |
// | BW-01 | Happy    | /btw asks with the conversation so far and only its tools; its answer is a pill that opens, not kept | P0 |
// | BW-02 | Happy    | /btw! tells the main agent what it changed, which it reads at its next step     | P0       |
// | BW-03 | Edge     | While /btw answers, its state line shows; a second /btw waits; /btw alone stops it | P1     |
// | BW-06 | Happy    | The answer streams into its opened block at the spinner's tick, and into its box | P0      |
// | BW-07 | Happy    | Files attached to /btw reach the model as paths, as a prompt's do              | P0       |
// | BW-08 | Happy    | /btw open and a click on its line open the box; esc closes it; right click offers it | P1   |
// | BW-04 | Happy    | What /btw asks says /btw asks it                                               | P1       |
// | BW-05 | Happy    | In the tool pick, a * that ticks everything spreads so one tool can be unticked | P1       |

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// recording answers with its replies in turn, and keeps the requests.
type recording struct {
	mu       sync.Mutex
	replies  []llm.Message
	requests []llm.Request
}

func (r *recording) Stream(_ context.Context, request llm.Request, _ func(llm.Event)) (llm.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
	reply := r.replies[0]
	r.replies = r.replies[1:]
	return llm.Response{Message: reply}, nil
}

func btwModel(t *testing.T, provider llm.Provider) (model, string) {
	t.Helper()
	m := savedModel(t)
	workDir := m.session.WorkDir
	m.session.Agent.Provider = provider
	m.session.Agent.History = slices.Clone(chat)
	m.session.Agent.Tools = tools.NewRegistry(tools.Env{WorkDir: workDir}, tools.Options{MaxOutput: 1000})
	m.session.BtwTools = func(bang bool) *tools.Registry {
		disabled := []string{"bash"}
		if bang {
			disabled = nil
		}
		return tools.NewRegistry(tools.Env{WorkDir: workDir}, tools.Options{Disabled: disabled, MaxOutput: 1000})
	}
	return m, workDir
}

// answered runs the command of /btw and gives the model its answer.
func answered(t *testing.T, m model, command string) model {
	t.Helper()
	updated, cmd := m.runCommand(command)
	m = updated.(model)
	for _, msg := range everyMsg(cmd) {
		if done, ok := msg.(btwDoneMsg); ok {
			updated, _ = m.Update(done)
			return updated.(model)
		}
	}
	t.Fatal("no answer")
	return m
}

func everyMsg(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, each := range batch {
		msgs = append(msgs, everyMsg(each)...)
	}
	return msgs
}

func specNames(specs []llm.ToolSpec) []string {
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func TestBtw(t *testing.T) {
	t.Run("BW-01 /btw asks with the conversation so far and only its tools; the answer is shown, not kept", func(t *testing.T) {
		provider := &recording{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "It listed six files."}}}
		m, _ := btwModel(t, provider)

		m = answered(t, m, "/btw what did you do?")

		sent := provider.requests[0]
		if len(sent.Messages) != len(chat)+1 || !strings.HasPrefix(sent.Messages[len(chat)].Text, "what did you do?") {
			t.Errorf("sent %+v", sent.Messages)
		}
		if names := specNames(sent.Tools); slices.Contains(names, "bash") || !slices.Contains(names, "read") {
			t.Errorf("tools %v", names)
		}
		folded := shown(m)
		m.toggleBtw(m.blocks[len(m.blocks)-1].btw)
		opened := shown(m)
		if !strings.Contains(folded, "btw  what did you do? · 1 line") || strings.Contains(folded, "It listed six files.") {
			t.Errorf("folded:\n%s", folded)
		}
		if !strings.Contains(opened, "│") || !strings.Contains(opened, "It listed six files.") || !strings.Contains(opened, "not kept in the conversation") {
			t.Errorf("opened:\n%s", opened)
		}
		if len(m.session.Agent.History) != len(chat) || m.btw != nil {
			t.Errorf("history %d, btw %v", len(m.session.Agent.History), m.btw)
		}
	})

	t.Run("BW-02 /btw! tells the main agent what it changed, which it reads at its next step", func(t *testing.T) {
		provider := &recording{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "w1", Name: "write", Args: []byte(`{"path":"notes.txt","content":"hi"}`)}}},
			{Role: llm.RoleAssistant, Text: "Written."},
			{Role: llm.RoleAssistant, Text: "Noted."},
		}}
		m, workDir := btwModel(t, provider)

		m = answered(t, m, "/btw! write notes.txt")
		if err := m.session.Agent.Run(context.Background(), "go on", agent.Hooks{}); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(workDir, "notes.txt")); err != nil {
			t.Fatal(err)
		}
		told := provider.requests[2].Messages
		if last := told[len(told)-1].Text; !strings.Contains(last, "/btw!") || !strings.Contains(last, "write") || !strings.Contains(last, "notes.txt") {
			t.Errorf("told %q", last)
		}
		m.toggleBtw(m.blocks[len(m.blocks)-1].btw)
		if view := shown(m); !strings.Contains(view, "the model is told it did: write") {
			t.Errorf("shown:\n%s", view)
		}
	})

	t.Run("BW-03 while /btw answers, its state line shows; a second /btw waits; /btw alone stops it", func(t *testing.T) {
		m, _ := btwModel(t, &recording{})
		stopped := false
		m.btw = &btwRun{answer: &btwAnswer{question: "q"}, cancel: func() { stopped = true }, doing: "read a.go"}

		state, _ := m.upperLayout()
		again, _ := m.runCommand("/btw and now?")
		m = again.(model)
		m.runCommand("/btw")

		if line := ansi.Strip(state); !strings.Contains(line, "btw · read a.go") || !strings.Contains(line, "esc to stop") {
			t.Errorf("state %q", line)
		}
		if !strings.Contains(shown(m), "answering already") || !stopped {
			t.Errorf("stopped %v, shown:\n%s", stopped, shown(m))
		}
	})

	t.Run("BW-04 what /btw asks says /btw asks it", func(t *testing.T) {
		fromBtw, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{bash("make test")}, From: btwAsker})

		if view := ansi.Strip(fromBtw.dialogView()); !strings.Contains(view, "asked by /btw") {
			t.Errorf("view:\n%s", view)
		}
	})

	t.Run("BW-05 in the tool pick, a * that ticks everything spreads so one tool can be unticked", func(t *testing.T) {
		chosen := []string{"*"}
		pick := newToolPick([]string{"read", "bash", "mcp__xdr__search"}, []string{"xdr"}, &chosen)

		pick.toggle("bash")

		if !slices.Equal(chosen, []string{"read", "mcp__xdr__*"}) {
			t.Errorf("chosen %q", chosen)
		}
	})
}

func TestBtwLive(t *testing.T) {
	t.Run("BW-06 the answer streams into its opened block at the spinner's tick, and into its box", func(t *testing.T) {
		m, _ := btwModel(t, &recording{})
		answer := &btwAnswer{question: "why?"}
		m.btw = &btwRun{answer: answer, cancel: func() {}}
		m.addBtwBlock(answer)
		m.toggleBtw(answer)

		updated, _ := m.Update(btwEventMsg{Kind: llm.EventText, Text: "Because the"})
		before := shown(updated.(model))
		updated, _ = updated.(model).Update(btwEventMsg{Kind: llm.EventText, Text: " cache was stale."})
		updated, _ = updated.(model).Update(m.spinner.Tick())
		m = updated.(model)
		opened, _ := m.openBtwBox(answer)
		box := ansi.Strip(opened.(model).View().Content)

		if strings.Contains(before, "Because the") {
			t.Errorf("drawn before the tick:\n%s", before)
		}
		if view := shown(m); !strings.Contains(view, "Because the cache was stale.") || !strings.Contains(view, "answering") {
			t.Errorf("shown:\n%s", view)
		}
		if !strings.Contains(box, "Because the cache was stale.") || !strings.Contains(box, "esc closes") {
			t.Errorf("box:\n%s", box)
		}
	})

	t.Run("BW-07 files attached to /btw reach the model as paths, as a prompt's do", func(t *testing.T) {
		provider := &recording{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "A cat."}}}
		m, workDir := btwModel(t, provider)
		picture := filepath.Join(workDir, "cat.png")
		if err := os.WriteFile(picture, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		m.attach([]string{picture}, true)

		m = answered(t, m, "/btw [image #1] what is it?")

		sent := provider.requests[0].Messages[len(chat)]
		if strings.Contains(sent.Text, "[image #1]") || (!strings.Contains(sent.Text, picture) && len(sent.Media) == 0) {
			t.Errorf("sent %q, %d media", sent.Text, len(sent.Media))
		}
		if len(m.attachments) != 0 {
			t.Errorf("attachments left %v", m.attachments)
		}
	})

	t.Run("BW-08 /btw open and a click on its line open the box; esc closes it; right click offers it", func(t *testing.T) {
		m, _ := btwModel(t, &recording{})
		answer := &btwAnswer{question: "why?", text: "Because.", done: true}
		m.addBtwBlock(answer)

		opened, _ := m.runCommand("/btw open")
		closed, _ := opened.(model).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.btw = &btwRun{answer: answer, cancel: func() {}}
		_, regions := m.btwStateLine()
		clicked, _ := regions[0].do(m)
		menu, _ := m.btwMenu(answer)

		if opened.(model).btwBox == nil || closed.(model).btwBox != nil || clicked.(model).btwBox == nil {
			t.Errorf("box: opened %v closed %v clicked %v", opened.(model).btwBox, closed.(model).btwBox, clicked.(model).btwBox)
		}
		if view := ansi.Strip(menu.(model).View().Content); !strings.Contains(view, "open in a box") {
			t.Errorf("menu:\n%s", view)
		}
	})
}
