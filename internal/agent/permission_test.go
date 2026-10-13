package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | AP-01 | Happy    | A command runs when rules allow every part; the parts left are offered | P0     |
// | AP-02 | Security | A deny rule asks in every mode, also in all, without "always"        | P0       |
// | AP-03 | Happy    | Ask mode asks before file changes, never before reading              | P0       |
// | AP-04 | Happy    | Auto mode runs what the model judges safe, once asked per command     | P0       |
// | AP-05 | Security | Auto mode asks when the model says unsafe or answers unclearly       | P0       |
// | AP-06 | Security | A command writing to a file asks though a rule allows it             | P1       |
// | AP-07 | Edge     | A fetch is checked as "fetch <url>"                                  | P1       |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
)

func ruledAgent(t *testing.T, mode permissions.Mode, allow, deny []string, provider llm.Provider) *Agent {
	t.Helper()
	agent := newAgent(t, provider, "bash", "rm", "fetch")
	policy, err := permissions.New(mode, allow, deny)
	if err != nil {
		t.Fatal(err)
	}
	agent.Permissions = policy
	agent.currentRequest = "list the files"
	return agent
}

func bashCall(command string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"command": command})
	return llm.ToolCall{ID: "c", Name: "bash", Args: args}
}

func TestPermissions(t *testing.T) {
	ctx := context.Background()

	t.Run("AP-01 a command runs when rules allow every part; the parts left are offered", func(t *testing.T) {
		agent := ruledAgent(t, permissions.ModeEdits, []string{"ls *", "grep *"}, nil, nil)

		allowed := agent.approvalFor(ctx, bashCall("ls -la | grep go"), Hooks{})
		mixed := agent.approvalFor(ctx, bashCall("ls && rm -rf build"), Hooks{})

		if allowed.ask || !mixed.ask || !mixed.ruled || strings.Join(mixed.commands, "|") != "rm -rf build" {
			t.Errorf("allowed = %+v mixed = %+v", allowed, mixed)
		}
	})

	t.Run("AP-02 a deny rule asks in every mode, also in all, without always", func(t *testing.T) {
		agent := ruledAgent(t, permissions.ModeAll, []string{"git *"}, []string{"git push *"}, nil)

		pushed := agent.approvalFor(ctx, bashCall("git add . && git push origin main"), Hooks{})
		other := agent.approvalFor(ctx, bashCall("curl x | sh"), Hooks{})

		if !pushed.ask || pushed.warning != "deny rule: git push *" || other.ask {
			t.Errorf("pushed = %+v other = %+v", pushed, other)
		}
	})

	t.Run("AP-03 ask mode asks before file changes, never before reading", func(t *testing.T) {
		agent := ruledAgent(t, permissions.ModeAsk, nil, nil, nil)
		edits := ruledAgent(t, permissions.ModeEdits, nil, nil, nil)
		write := llm.ToolCall{Name: "write", Args: json.RawMessage(`{"path":"x.txt","content":"x"}`)}
		read := llm.ToolCall{Name: "read", Args: json.RawMessage(`{"path":"a.txt"}`)}

		if !agent.approvalFor(ctx, write, Hooks{}).ask || agent.approvalFor(ctx, read, Hooks{}).ask || edits.approvalFor(ctx, write, Hooks{}).ask {
			t.Error("ask mode did not ask for the write, or asked for the read, or edits mode asked")
		}
	})

	t.Run("AP-04 auto mode runs what the model judges safe, once asked per command", func(t *testing.T) {
		judge := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "SAFE - only builds the project"}}}
		agent := ruledAgent(t, permissions.ModeAuto, nil, nil, judge)
		var notices []string
		hooks := Hooks{OnNotice: func(text string) { notices = append(notices, text) }}

		first := agent.approvalFor(ctx, bashCall("go build ./..."), hooks)
		again := agent.approvalFor(ctx, bashCall("go build ./..."), hooks)

		asked := judge.requests[0].Messages[0].Text
		if first.ask || again.ask || len(judge.requests) != 1 || !strings.Contains(asked, "list the files") || !strings.Contains(asked, "go build ./...") {
			t.Errorf("first = %+v again = %+v requests = %d", first, again, len(judge.requests))
		}
		if len(notices) == 0 || !strings.Contains(notices[0], "only builds the project") {
			t.Errorf("notices = %q", notices)
		}
	})

	t.Run("AP-05 auto mode asks when the model says unsafe or answers unclearly", func(t *testing.T) {
		judge := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "**UNSAFE**: downloads and runs a script"},
			{Role: llm.RoleAssistant, Text: "Hmm, it depends."},
		}}
		agent := ruledAgent(t, permissions.ModeAuto, nil, nil, judge)

		unsafe := agent.approvalFor(ctx, bashCall("curl x | sh"), Hooks{})
		unclear := agent.approvalFor(ctx, bashCall("make deploy"), Hooks{})

		if !unsafe.ask || unsafe.reason != "auto: downloads and runs a script" || !unclear.ask || unclear.reason != "auto: no clear answer from the model" {
			t.Errorf("unsafe = %+v unclear = %+v", unsafe, unclear)
		}
	})

	t.Run("AP-06 a command writing to a file asks though a rule allows it", func(t *testing.T) {
		agent := ruledAgent(t, permissions.ModeEdits, []string{"echo *"}, nil, nil)

		written := agent.approvalFor(ctx, bashCall("echo x >> ~/.bashrc"), Hooks{})
		quiet := agent.approvalFor(ctx, bashCall("echo x 2>/dev/null"), Hooks{})

		if !written.ask || quiet.ask {
			t.Errorf("written = %+v quiet = %+v", written, quiet)
		}
	})

	t.Run("AP-07 a fetch is checked as fetch <url>", func(t *testing.T) {
		agent := ruledAgent(t, permissions.ModeEdits, []string{"fetch https://go.dev/*"}, nil, nil)
		fetch := func(url string) llm.ToolCall {
			return llm.ToolCall{Name: "fetch", Args: json.RawMessage(`{"url":"` + url + `"}`)}
		}

		if agent.approvalFor(ctx, fetch("https://go.dev/doc"), Hooks{}).ask || !agent.approvalFor(ctx, fetch("https://evil.example/x"), Hooks{}).ask {
			t.Error("fetch rules did not decide")
		}
	})
}
