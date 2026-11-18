package tui

// Test Traceability Table
// | ID    | Category | Description                                                                    | Priority |
// | BW-01 | Happy    | /btw asks with the conversation so far and only its tools; the answer is shown, not kept | P0 |
// | BW-02 | Happy    | /btw! tells the main agent what it changed, which it reads at its next step     | P0       |
// | BW-03 | Edge     | While /btw answers, its state line shows; a second /btw waits; /btw alone stops it | P1     |
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
		if view := shown(m); !strings.Contains(view, "/btw what did you do?") || !strings.Contains(view, "It listed six files.") {
			t.Errorf("shown:\n%s", view)
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
		if view := shown(m); !strings.Contains(view, "btw! did") {
			t.Errorf("shown:\n%s", view)
		}
	})

	t.Run("BW-03 while /btw answers, its state line shows; a second /btw waits; /btw alone stops it", func(t *testing.T) {
		m, _ := btwModel(t, &recording{})
		stopped := false
		m.btw = &btwRun{cancel: func() { stopped = true }, doing: "read a.go"}

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
