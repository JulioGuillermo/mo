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
// | AP-08 | Security | A tool reached through tool_call asks as itself; the UI sees the tool | P0      |
// | AP-09 | Security | A PowerShell command asks though a rule allows it; deny rules apply  | P0       |
// | AP-10 | Happy    | In auto mode the model judges a PowerShell command as a whole        | P1       |
// | AP-11 | Security | A !!! rule refuses the call without asking, in every mode, and tells the model why | P0 |
// | AP-12 | Happy    | A ! exception asks in edits, goes to the judge in auto, runs in all; !! asks in all | P0 |
// | AP-13 | Happy    | mo's own rules and the user's add up                                 | P1       |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

func ruledAgent(t *testing.T, mode permissions.Mode, allow, deny []string, provider llm.Provider) *Agent {
	t.Helper()
	agent := newAgent(t, provider, "bash", "rm", "web_fetch")
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

	t.Run("AP-07 a fetch is checked as web_fetch <url>, and the rules written as fetch <url> still decide", func(t *testing.T) {
		fetch := func(name, url string) llm.ToolCall {
			return llm.ToolCall{Name: name, Args: json.RawMessage(`{"url":"` + url + `"}`)}
		}
		asks := func(agent *Agent, name, url string) bool {
			return agent.approvalFor(ctx, fetch(name, url), Hooks{}).ask
		}
		current := ruledAgent(t, permissions.ModeEdits, []string{"web_fetch https://go.dev/*"}, nil, nil)
		legacy := ruledAgent(t, permissions.ModeEdits, []string{"fetch https://go.dev/*"}, nil, nil)
		denied := ruledAgent(t, permissions.ModeAll, nil, []string{"fetch https://evil.example/*"}, nil)
		denyRegex := ruledAgent(t, permissions.ModeAll, nil, []string{"re:^fetch https://evil"}, nil)

		for name, agent := range map[string]*Agent{"web_fetch rule": current, "fetch rule": legacy} {
			if asks(agent, "web_fetch", "https://go.dev/doc") || !asks(agent, "web_fetch", "https://evil.example/x") {
				t.Errorf("%s did not decide", name)
			}
		}
		if verdict, _ := denied.Permissions.Check("web_fetch https://evil.example/x"); verdict != permissions.Denied {
			t.Error("a deny rule written as fetch stopped applying")
		}
		if verdict, _ := denyRegex.Permissions.Check("web_fetch https://evil.example/x"); verdict != permissions.Denied {
			t.Error("a deny rule written as a regex on fetch stopped applying")
		}
	})
	t.Run("AP-08 a tool reached through tool_call asks as itself; the UI sees the tool", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "w", Name: "tool_call", Args: json.RawMessage(`{"name":"bash","args":{"command":"git push origin main"}}`)}}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent, _ := newAgentIn(t, provider, "bash")
		agent.Tools = tools.NewRegistry(tools.Env{WorkDir: t.TempDir()}, tools.Options{Mode: tools.ModeSearch, MaxOutput: 1000})
		agent.Permissions, _ = permissions.New(permissions.ModeAll, nil, []string{"git push *"})
		var asked []ApprovalRequest
		var started []string
		hooks := Hooks{
			Approve:     func(_ context.Context, request ApprovalRequest) Decision { asked = append(asked, request); return Deny },
			OnToolStart: func(call llm.ToolCall, _ string) { started = append(started, call.Name) },
		}

		_ = agent.Run(ctx, "push it", hooks)

		if len(asked) != 1 || asked[0].Call.Name != "bash" || asked[0].Warning != "deny rule: git push *" || len(started) != 0 {
			t.Errorf("asked = %+v started = %v", asked, started)
		}
		if result := agent.History[2]; result.ToolCallID != "w" || result.Text != deniedResult {
			t.Errorf("result = %+v", result)
		}
	})

	t.Run("AP-09 a PowerShell command asks though a rule allows it; deny rules apply", func(t *testing.T) {
		defer tools.UseShell(tools.PowerShellForTests(false))()
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "l", Name: "powershell", Args: json.RawMessage(`{"command":"Get-ChildItem -Force"}`)},
				{ID: "r", Name: "powershell", Args: json.RawMessage(`{"command":"Remove-Item -Recurse build"}`)},
			}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent, _ := newAgentIn(t, provider, "bash")
		agent.Permissions, _ = permissions.New(permissions.ModeEdits, []string{"Get-ChildItem *", "*"}, []string{"Remove-Item *"})
		var asked []ApprovalRequest
		hooks := Hooks{Approve: func(_ context.Context, request ApprovalRequest) Decision { asked = append(asked, request); return Deny }}

		_ = agent.Run(ctx, "list the files", hooks)

		if len(asked) != 2 || asked[0].Call.Name != "bash" || strings.Join(asked[0].Commands, "|") != "Get-ChildItem -Force" || asked[1].Warning != "deny rule: Remove-Item *" {
			t.Errorf("asked = %+v", asked)
		}
	})

	t.Run("AP-10 in auto mode the model judges a PowerShell command as a whole", func(t *testing.T) {
		defer tools.UseShell(tools.PowerShellForTests(false))()
		judge := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "SAFE - lists files"}}}
		agent := ruledAgent(t, permissions.ModeAuto, nil, nil, judge)

		listed := agent.approvalFor(ctx, bashCall("Get-ChildItem | Select-Object Name"), Hooks{})

		if listed.ask || len(judge.requests) != 1 || !strings.Contains(judge.requests[0].Messages[0].Text, "Get-ChildItem | Select-Object Name") {
			t.Errorf("listed = %+v requests = %d", listed, len(judge.requests))
		}
	})

	t.Run("AP-11 a !!! rule refuses the call without asking, in every mode, and tells the model why", func(t *testing.T) {
		for _, mode := range permissions.Modes {
			provider := &scriptedProvider{replies: []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{bashCall("ls && rm -rf /etc")}},
				{Role: llm.RoleAssistant, Text: "done"},
			}}
			agent, _ := newAgentIn(t, provider, "bash")
			agent.Permissions, _ = permissions.New(mode, []string{"!!!rm -rf /*"}, nil)
			asked := 0
			hooks := Hooks{Approve: func(context.Context, ApprovalRequest) Decision { asked++; return Allow }}

			_ = agent.Run(ctx, "clean up", hooks)

			result := agent.History[2]
			if asked != 0 || !strings.Contains(result.Text, `blocked by the rule "!!!rm -rf /*"`) || !strings.Contains(result.Text, "rm -rf /etc") {
				t.Errorf("%s: asked %d result %q", mode, asked, result.Text)
			}
		}
	})

	t.Run("AP-12 a ! exception asks in edits, goes to the judge in auto, runs in all; !! asks in all", func(t *testing.T) {
		rules := []string{"ls *", "!ls .git*", "!!git push *"}
		edits := ruledAgent(t, permissions.ModeEdits, rules, nil, nil)
		judge := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "SAFE - reads"}}}
		auto := ruledAgent(t, permissions.ModeAuto, rules, nil, judge)
		all := ruledAgent(t, permissions.ModeAll, rules, nil, nil)

		inEdits := edits.approvalFor(ctx, bashCall("ls .git"), Hooks{})
		inAuto := auto.approvalFor(ctx, bashCall("ls -la && ls .git"), Hooks{})
		inAll := all.approvalFor(ctx, bashCall("ls .git"), Hooks{})
		pushInAll := all.approvalFor(ctx, bashCall("git push origin"), Hooks{})

		if !inEdits.ask || inAuto.ask || len(judge.requests) != 1 || strings.Contains(judge.requests[0].Messages[0].Text, "ls -la") || inAll.ask {
			t.Errorf("edits %+v auto %+v all %+v", inEdits, inAuto, inAll)
		}
		if !pushInAll.ask || pushInAll.warning != "deny rule: !!git push *" {
			t.Errorf("push in all %+v", pushInAll)
		}
	})

	t.Run("AP-13 mo's own rules and the user's add up", func(t *testing.T) {
		rules := config.Permissions{Allow: []string{"make *", "!cat .env*"}}.Rules()
		agent := ruledAgent(t, permissions.ModeEdits, rules, nil, nil)

		both := agent.approvalFor(ctx, bashCall("cat README.md && make test"), Hooks{})
		secret := agent.approvalFor(ctx, bashCall("cat .env"), Hooks{})

		if both.ask || !secret.ask {
			t.Errorf("both %+v secret %+v", both, secret)
		}
	})
}
