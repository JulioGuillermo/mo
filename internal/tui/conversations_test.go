package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | TL-01 | Happy    | A tool call is one block: first lines folded, all of it open, with its result | P0 |
// | TL-02 | Happy    | ctrl+o opens tool calls too; a click opens only the one under it     | P1       |
// | CV-01 | Happy    | A turn's end saves the conversation; resume brings it back drawn whole | P0     |
// | CV-02 | Happy    | Rewind goes on in a fork before the prompt, which waits in the input  | P0       |
// | CV-03 | Happy    | Fork saves a copy naming the original; /new starts a new one on a clear screen | P1     |
// | CV-04 | Happy    | esc twice with an empty input opens the prompts to go back to        | P1       |
// | CV-05 | Happy    | Continuing loads the latest conversation of the folder at once       | P1       |
// | CV-06 | Happy    | /delete asks, then deletes the conversation and its output, and starts anew | P0 |
// | CV-07 | Happy    | ctrl+x in /resume asks before deleting; no keeps it, yes deletes it   | P0       |
// | CV-08 | Happy    | /clean deletes, after asking, what was last used before a date or an age | P1    |
// | CV-09 | Edge     | A fork starts with the output of the commands of the one it came from | P1       |

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

func savedModel(t *testing.T) model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := conversationModel()
	m.session.Agent = &agent.Agent{}
	m.session.WorkDir = t.TempDir()
	return m
}

var chat = []llm.Message{
	{Role: llm.RoleUser, Text: "list the files"},
	{Role: llm.RoleAssistant, Reasoning: "use ls", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "bash", Args: []byte(`{"command":"ls"}`)}}},
	{Role: llm.RoleTool, ToolCallID: "c1", Text: "a.go\nb.go\nc.go\nd.go\ne.go\nf.go"},
	{Role: llm.RoleAssistant, Text: "Six files."},
	{Role: llm.RoleUser, Text: "and the tests?"},
	{Role: llm.RoleAssistant, Text: "None."},
}

func run(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, each := range batch {
			if each != nil {
				m = run(t, m, each)
			}
		}
		return m
	}
	updated, next := m.Update(msg)
	return run(t, updated.(model), next)
}

func TestToolBlocks(t *testing.T) {
	t.Run("TL-01 a tool call is one block: first lines folded, all of it open, with its result", func(t *testing.T) {
		m := conversationModel()
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "x", Name: "bash"}, summary: "go test ./..."})
		running := shown(started.(model))
		done, _ := started.(model).Update(toolDoneMsg{id: "x", output: "1\n2\n3\n4\n5\n6\n7"})
		folded := shown(done.(model))
		opened, _ := done.(model).Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

		if len(done.(model).blocks) != 1 || !strings.Contains(running, "running…") || strings.Contains(folded, "running") {
			t.Errorf("running:\n%s\nfolded:\n%s", running, folded)
		}
		if strings.Contains(folded, "    7") || !strings.Contains(folded, "+3 lines") || !strings.Contains(folded, "whole call and output") || !strings.Contains(shown(opened.(model)), "    7") {
			t.Errorf("folded:\n%s\nopened:\n%s", folded, shown(opened.(model)))
		}
	})

	t.Run("TL-02 ctrl+o opens tool calls too; a click opens only the one under it", func(t *testing.T) {
		m := conversationModel()
		for _, id := range []string{"a", "b"} {
			updated, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: id, Name: "bash"}, summary: "echo " + id})
			updated, _ = updated.(model).Update(toolDoneMsg{id: id, output: strings.Repeat(id+"\n", 9)})
			m = updated.(model)
		}
		row := strings.Index(shown(m), "echo a")
		viewport := m.conversation(m.height - 1)
		row = strings.Count(shown(m)[:row], "\n") - viewport.YOffset()

		clicked := mouseClick(m, 2, row)

		if !clicked.(model).blocks[0].open || clicked.(model).blocks[1].open {
			t.Errorf("open = %v %v", clicked.(model).blocks[0].open, clicked.(model).blocks[1].open)
		}
	})
}

func TestConversations(t *testing.T) {
	t.Run("CV-01 a turn's end saves the conversation; resume brings it back drawn whole", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = chat
		m.session.Agent.LastUsage = llm.Usage{InputTokens: 120}
		m.running, m.cancel = true, func() {}
		updated, save := m.finishRun(nil)
		m = run(t, updated.(model), save)
		infos, _ := sessions.List(config.SessionDir(), config.ProjectKey(m.session.WorkDir))

		fresh := m
		fresh.session.Agent = &agent.Agent{}
		fresh.blocks, fresh.thread = nil, conversation{}
		fresh = run(t, fresh, loadConversation(infos[0].ID))

		drawn := shown(fresh)
		if len(infos) != 1 || infos[0].Title != "list the files" || infos[0].Prompts != 2 || len(fresh.session.Agent.History) != 6 || fresh.usage.InputTokens != 120 {
			t.Fatalf("infos = %+v history = %d", infos, len(fresh.session.Agent.History))
		}
		for _, want := range []string{"list the files", "▸ reasoning", "● bash", "a.go", "Six files.", "and the tests?", "resumed: list the files"} {
			if !strings.Contains(drawn, want) {
				t.Errorf("missing %q in:\n%s", want, drawn)
			}
		}
	})

	t.Run("CV-02 rewind goes on in a fork before the prompt, which waits in the input", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = chat
		m = run(t, m, m.saveConversation())
		original := m.thread.id

		listed, _ := m.pickPrompt()
		view := listed.(model).settings.form.View()
		rewound, _ := listed.(model).settings.next(listed.(model))
		after := rewound.(model)

		if !strings.Contains(view, "› and the tests?") || !strings.Contains(view, "› list the files") {
			t.Errorf("picker:\n%s", view)
		}
		if after.input.Value() != "and the tests?" || len(after.session.Agent.History) != 4 || after.thread.parent != original || after.thread.id != "" || strings.Contains(shown(after), "None.") {
			t.Errorf("input = %q history = %d thread = %+v", after.input.Value(), len(after.session.Agent.History), after.thread)
		}
		kept, _ := sessions.Load(config.SessionDir(), original)
		if len(kept.History) != 6 {
			t.Errorf("the original lost messages: %d", len(kept.History))
		}
	})

	t.Run("CV-03 fork saves a copy naming the original; /new starts a new one on a clear screen", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = chat
		m = run(t, m, m.saveConversation())
		original := m.thread.id

		forked, cmd := m.fork()
		m = run(t, forked.(model), cmd)
		infos, _ := sessions.List(config.SessionDir(), config.ProjectKey(m.session.WorkDir))
		m.replay(chat)
		cleared, _ := m.runCommand("/new")
		alias, _ := m.runCommand("/clear")
		if strings.Contains(ansi.Strip(shown(cleared.(model))), "list the files") || alias.(model).thread.id != "" {
			t.Errorf("the screen should start clear, and /clear still work:\n%s", shown(cleared.(model)))
		}

		if len(infos) != 2 || m.thread.parent != original || m.thread.id == original || len(cleared.(model).session.Agent.History) != 0 || cleared.(model).thread.id != "" {
			t.Errorf("infos = %d thread = %+v", len(infos), m.thread)
		}
	})

	t.Run("CV-04 esc twice with an empty input opens the prompts to go back to", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = chat

		once, _ := m.Update(keyEsc)
		twice, _ := once.(model).Update(keyEsc)

		if once.(model).settings != nil || twice.(model).settings == nil || !strings.Contains(twice.(model).settings.form.View(), "Go back to a prompt") {
			t.Error("esc esc did not open the prompts")
		}
	})

	t.Run("CV-05 continuing loads the latest conversation of the folder at once", func(t *testing.T) {
		m := savedModel(t)
		for i, title := range []string{"older", "newer"} {
			_ = sessions.Save(config.SessionDir(), sessions.Record{
				Info:    sessions.Info{ID: fmt.Sprint("s", i), WorkDir: config.ProjectKey(m.session.WorkDir), Title: title, Updated: timeAt(i)},
				History: []llm.Message{{Role: llm.RoleUser, Text: title}},
			})
		}

		m = run(t, m, listConversations(m.session.WorkDir, true))

		if m.thread.title != "newer" || !strings.Contains(ansi.Strip(shown(m)), "newer") {
			t.Errorf("thread = %+v", m.thread)
		}
	})
}

func TestDeleting(t *testing.T) {
	keyYes := tea.KeyPressMsg{Code: 'y', Text: "y"}
	keyNo := tea.KeyPressMsg{Code: 'n', Text: "n"}
	saveAt := func(t *testing.T, m model, id string, updated time.Time) {
		t.Helper()
		_ = sessions.Save(config.SessionDir(), sessions.Record{
			Info:    sessions.Info{ID: id, WorkDir: config.ProjectKey(m.session.WorkDir), Title: id, Updated: updated},
			History: []llm.Message{{Role: llm.RoleUser, Text: id}},
		})
		_ = os.MkdirAll(sessions.JobsDir(config.SessionDir(), id), 0o700)
	}
	exists := func(id string) bool {
		_, err := os.Stat(filepath.Join(config.SessionDir(), id+".json"))
		_, dirErr := os.Stat(filepath.Join(config.SessionDir(), id))
		return err == nil || dirErr == nil
	}

	t.Run("CV-06 /delete asks, then deletes the conversation and its output, and starts anew", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = chat
		m = run(t, m, m.saveConversation())
		id := m.thread.id
		_ = os.MkdirAll(sessions.JobsDir(config.SessionDir(), id), 0o700)

		asked, _ := m.runCommand("/delete")
		question := asked.(model).settings.form.View()
		done, cmd := asked.(model).Update(keyYes)
		m = run(t, done.(model), cmd)

		if !strings.Contains(question, "Delete") || exists(id) || m.thread.id != "" || len(m.session.Agent.History) != 0 {
			t.Errorf("question = %q still there = %v thread = %+v", question, exists(id), m.thread)
		}
	})

	t.Run("CV-07 ctrl+x in /resume asks before deleting; no keeps it, yes deletes it", func(t *testing.T) {
		m := savedModel(t)
		saveAt(t, m, "only", time.Now())
		m = run(t, m, listConversations(m.session.WorkDir, false))
		if !strings.Contains(m.settings.form.View(), removeMark) {
			t.Fatalf("no delete mark in:\n%s", m.settings.form.View())
		}

		asked, _ := m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		kept, cmd := asked.(model).Update(keyNo)
		kept = run(t, kept.(model), cmd)
		if !exists("only") || kept.(model).settings == nil {
			t.Fatal("no should keep it and show the list again")
		}
		again, _ := kept.(model).Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		gone, cmd := again.(model).Update(keyYes)
		run(t, gone.(model), cmd)

		if exists("only") {
			t.Error("yes did not delete it")
		}
	})

	t.Run("CV-08 /clean deletes, after asking, what was last used before a date or an age", func(t *testing.T) {
		m := savedModel(t)
		saveAt(t, m, "ancient", time.Now().AddDate(0, -3, 0))
		saveAt(t, m, "recent", time.Now().AddDate(0, 0, -3))

		asked, _ := m.runCommand("/clean 2 months")
		question := asked.(model).settings.form.View()
		done, cmd := asked.(model).Update(keyYes)
		run(t, done.(model), cmd)

		if !strings.Contains(question, "Delete 1 conversations?") || exists("ancient") || !exists("recent") {
			t.Errorf("question = %q ancient = %v recent = %v", question, exists("ancient"), exists("recent"))
		}
		now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)
		for argument, want := range map[string]time.Time{
			"2 months": now.AddDate(0, -2, 0), "3 semanas": now.AddDate(0, 0, -21), "10d": now.AddDate(0, 0, -10),
			"1 año": now.AddDate(-1, 0, 0), "2026-08-01": time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local),
		} {
			if got, err := parseCutoff(argument, now); err != nil || !got.Equal(want) {
				t.Errorf("%q = %v, %v", argument, got, err)
			}
		}
		if _, err := parseCutoff("soon", now); err == nil {
			t.Error("soon should not parse")
		}
	})

	t.Run("CV-09 a fork starts with the output of the commands of the one it came from", func(t *testing.T) {
		m := savedModel(t)
		m.session.Agent.History = chat
		m = run(t, m, m.saveConversation())
		original := m.thread.id
		dir := sessions.JobsDir(config.SessionDir(), original)
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(filepath.Join(dir, "1.log"), []byte("built\n"), 0o600)

		forked, cmd := m.fork()
		m = run(t, forked.(model), cmd)
		copied, err := os.ReadFile(filepath.Join(sessions.JobsDir(config.SessionDir(), m.thread.id), "1.log"))

		if err != nil || string(copied) != "built\n" {
			t.Errorf("copied = %q err = %v", copied, err)
		}
	})
}

func TestRemoveMark(t *testing.T) {
	line := "┃ > " + removeMark + " build the thing · 2 prompts"
	column := strings.Index(line, removeMark)
	at := len([]rune(line[:column]))
	if !onRemoveMark(line, at) || onRemoveMark(line, at+6) {
		t.Errorf("the mark is at column %d", at)
	}
}

func timeAt(hour int) time.Time {
	return time.Date(2026, 1, 1, hour, 0, 0, 0, time.UTC)
}
