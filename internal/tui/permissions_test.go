package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | PU-01 | Happy    | shift+tab goes through the modes and the status line shows the mode  | P0       |
// | PU-02 | Happy    | "a" on a command opens its parts as rules; saving allows it and keeps only the user's rules | P0 |
// | PU-03 | Edge     | Esc in the rule editor goes back to the question, still unanswered   | P1       |
// | PU-04 | Happy    | A narrow terminal drops provider and speeds before the mode and model | P1      |
// | PU-05 | Happy    | Settings keep the folder's rules apart and "try" names the deciding rule | P1   |
// | PU-06 | Error    | A broken rule in settings is refused with the field name            | P2       |

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
)

func ruledModel(t *testing.T) model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := conversationModel()
	policy, _ := permissions.New(permissions.ModeEdits, nil, nil)
	m.session.Agent = &agent.Agent{Window: 32000, Permissions: policy}
	m.session.WorkDir = t.TempDir()
	return m
}

func TestPermissionUI(t *testing.T) {
	t.Run("PU-01 shift+tab goes through the modes and the status line shows the mode", func(t *testing.T) {
		m := ruledModel(t)
		var seen []string
		for range 4 {
			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			m = updated.(model)
			seen = append(seen, string(m.session.Agent.Permissions.Mode()))
		}

		if strings.Join(seen, ",") != "auto,all,ask,edits" || !strings.Contains(ansi.Strip(m.statusLine()), "● edits") {
			t.Errorf("modes = %v status = %q", seen, ansi.Strip(m.statusLine()))
		}
	})

	t.Run("PU-02 a on a command opens its parts as rules; saving allows it and keeps only the user's rules", func(t *testing.T) {
		m := ruledModel(t)
		m, reply := withApproval(m, agent.ApprovalRequest{Call: llm.ToolCall{Name: "bash"}, Ruled: true, Commands: []string{"make test"}})

		editor, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		view := editor.(model).settings.form.View()
		saved, cmd := editor.(model).settings.next(editor.(model))

		verdict, _ := m.session.Agent.Permissions.Check("make test")
		if !strings.Contains(view, "make test") || decided(reply) != agent.Allow || saved.(model).dialog != nil || verdict != permissions.Allowed {
			t.Errorf("view:\n%s\nverdict = %v", view, verdict)
		}
		var done rulesSavedMsg
		for _, msg := range messages(cmd) {
			if saved, ok := msg.(rulesSavedMsg); ok {
				done = saved
			}
		}
		layer, _ := config.ReadUserLayer()
		if done.err != nil || !strings.Contains(strings.Join(layer.Permissions.Allow, "|"), "make test") || strings.Contains(strings.Join(layer.Permissions.Allow, "|"), "ls *") {
			t.Errorf("saved = %+v allow = %q", done, layer.Permissions.Allow)
		}
	})

	t.Run("PU-03 esc in the rule editor goes back to the question, still unanswered", func(t *testing.T) {
		m := ruledModel(t)
		m, reply := withApproval(m, agent.ApprovalRequest{Call: llm.ToolCall{Name: "bash"}, Ruled: true, Commands: []string{"make"}})

		editor, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		back, _ := editor.(model).Update(keyEsc)

		if back.(model).settings != nil || back.(model).dialog == nil || len(reply) != 0 {
			t.Error("esc did not go back to the question")
		}
	})

	t.Run("PU-04 a narrow terminal drops provider and speeds before the mode and model", func(t *testing.T) {
		m := ruledModel(t)
		m.session.Provider, m.session.Model = "llama-server", "Qwen3-8B"
		m.usage = llm.Usage{InputTokens: 16000}
		m.speed = llm.Usage{PromptRate: 900, GenerationRate: 40}
		m.width = 40

		status := ansi.Strip(m.statusLine())

		if ansi.StringWidth(status) > 40 || strings.Contains(status, "llama-server") || !strings.Contains(status, "edits") || !strings.Contains(status, "Qwen3-8B") || !strings.Contains(status, "50%") {
			t.Errorf("status = %q", status)
		}
	})

	t.Run("PU-05 settings keep the folder's rules apart and try names the deciding rule", func(t *testing.T) {
		file := config.Config{}
		draft := newSettingsDraft(file)
		draft.forProject("/work/app", file)
		draft.projectAllow, draft.deny = "make *", "make deploy*"
		draft.tryCommand = "make test && make deploy"

		layer, err := draft.applyPermissions(file)
		tried := draft.tryRules()

		if err != nil || layer.Permissions.Projects["/work/app"].Allow[0] != "make *" || layer.Permissions.Allow != nil {
			t.Errorf("layer = %+v err = %v", layer.Permissions, err)
		}
		if !strings.Contains(tried, "make test → runs (allow: make *)") || !strings.Contains(tried, "make deploy → asks always (make deploy*)") {
			t.Errorf("tried:\n%s", tried)
		}
	})

	t.Run("PU-06 a broken rule in settings is refused with the field name", func(t *testing.T) {
		draft := newSettingsDraft(config.Config{})
		draft.deny = "re:("

		_, err := draft.applyTo(config.Config{}, nil)

		if err == nil || !strings.Contains(err.Error(), "never without asking") {
			t.Errorf("err = %v", err)
		}
	})
}

// messages runs a command and the ones it batches, and returns their messages.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var all []tea.Msg
	for _, each := range batch {
		all = append(all, messages(each)...)
	}
	return all
}
