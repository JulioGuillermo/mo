package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CX-01 | Happy    | A cut of the context keeps the conversation as it was and goes on in a compact: one that names it | P0 |
// | CX-02 | Edge     | A second cut is titled compact: and the first title, not stacked, and names the one before | P1 |
// | CX-03 | Happy    | A fork and a rewind are titled fork: and the title of the one they came from | P0       |
// | CX-04 | Edge     | A title the user changed replaces fork: and compact:, and the prefixes do not stack | P0   |
// | CX-05 | Edge     | Between the cut and the step that follows, nothing of the old history is saved under the new one | P0 |
// | CX-06 | Happy    | The conversation as it was can be resumed whole                          | P0       |

import (
	"strings"
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

var summarized = []llm.Message{{Role: llm.RoleUser, Text: "Summary of the conversation so far:\nsix files, no tests"}}

func savedChat(t *testing.T) model {
	t.Helper()
	m := savedModel(t)
	m.session.Agent.History = chat
	m.running, m.cancel = true, func() {}
	updated, save := m.finishRun(nil)
	return run(t, updated.(model), save)
}

func saved(m model) []sessions.Info {
	infos, _ := sessions.List(config.SessionDir(), config.ProjectKey(m.session.WorkDir))
	return infos
}

func byTitle(infos []sessions.Info, title string) (sessions.Info, bool) {
	for _, info := range infos {
		if info.Title == title {
			return info, true
		}
	}
	return sessions.Info{}, false
}

func TestContextCutConversations(t *testing.T) {
	t.Run("CX-01 a cut of the context keeps the conversation as it was and goes on in a compact: one that names it", func(t *testing.T) {
		m := savedChat(t)
		oldID := m.thread.id

		m.session.Agent.History = summarized
		updated, cmd := m.Update(contextCutMsg(chat))
		m = run(t, updated.(model), cmd)
		m = run(t, m, m.saveConversation())
		infos := saved(m)

		old, hasOld := byTitle(infos, "list the files")
		child, hasChild := byTitle(infos, "compact: list the files")
		if len(infos) != 2 || !hasOld || !hasChild || old.ID != oldID || child.Parent != oldID || child.ID == oldID {
			t.Fatalf("infos = %+v", infos)
		}
		whole, _ := sessions.Load(config.SessionDir(), old.ID)
		next, _ := sessions.Load(config.SessionDir(), child.ID)
		if len(whole.History) != 6 || whole.History[5].Text != "None." || len(next.History) != 1 || !strings.Contains(shown(m), "stays in /resume") {
			t.Errorf("old = %d messages, new = %d, screen:\n%s", len(whole.History), len(next.History), shown(m))
		}
	})

	t.Run("CX-02 a second cut is titled compact: and the first title, not stacked, and names the one before", func(t *testing.T) {
		m := savedChat(t)
		m.session.Agent.History = summarized
		updated, cmd := m.Update(contextCutMsg(chat))
		m = run(t, updated.(model), cmd)
		m = run(t, m, m.saveConversation())
		firstChild := m.thread.id

		m.session.Agent.History = append(summarized, llm.Message{Role: llm.RoleUser, Text: "and then?"}, llm.Message{Role: llm.RoleAssistant, Text: "this"})
		again := append([]llm.Message{}, m.session.Agent.History...)
		m.session.Agent.History = summarized
		updated, cmd = m.Update(contextCutMsg(again))
		m = run(t, updated.(model), cmd)
		m = run(t, m, m.saveConversation())
		infos := saved(m)

		titled := 0
		for _, info := range infos {
			if info.Title == "compact: list the files" {
				titled++
			}
		}
		last, _ := sessions.Load(config.SessionDir(), m.thread.id)
		if len(infos) != 3 || titled != 2 || last.Parent != firstChild || last.Title != "compact: list the files" {
			t.Errorf("infos = %+v last = %+v", infos, last.Info)
		}
	})

	t.Run("CX-03 a fork and a rewind are titled fork: and the title of the one they came from", func(t *testing.T) {
		m := savedChat(t)

		forked, cmd := m.fork()
		m = run(t, forked.(model), cmd)
		prompts := m.session.Agent.Prompts()
		rewound, _ := m.rewind(prompts[1])

		if m.thread.title != "fork: list the files" || rewound.(model).thread.title != "fork: list the files" {
			t.Errorf("fork %q rewind %q", m.thread.title, rewound.(model).thread.title)
		}
		if _, ok := byTitle(saved(m), "fork: list the files"); !ok {
			t.Errorf("not saved as fork: %+v", saved(m))
		}
	})

	t.Run("CX-04 a title the user changed replaces fork: and compact:, and the prefixes do not stack", func(t *testing.T) {
		cases := map[string]string{
			"list the files":                "fork: list the files",
			"compact: list the files":       "fork: list the files",
			"fork: compact: list the files": "fork: list the files",
			"":                              "",
		}
		for title, want := range cases {
			if got := derivedTitle(forkPrefix, title); got != want {
				t.Errorf("fork of %q = %q, want %q", title, got, want)
			}
		}
		if derivedTitle(compactPrefix, "fork: x") != "compact: x" || !derivedFrom("fork: x") || !derivedFrom("compact: x") || derivedFrom("my own name") {
			t.Error("prefixes read wrong")
		}

		m := savedChat(t)
		forked, cmd := m.fork()
		m = run(t, forked.(model), cmd)
		renamed, cmd := m.setTitle("auth rework")
		m = run(t, renamed.(model), cmd)
		if _, ok := byTitle(saved(m), "auth rework"); !ok || m.thread.title != "auth rework" {
			t.Errorf("renamed: %q in %+v", m.thread.title, saved(m))
		}
		again, cmd := m.fork()
		m = run(t, again.(model), cmd)
		if m.thread.title != "fork: auth rework" {
			t.Errorf("fork of the renamed: %q", m.thread.title)
		}
	})

	t.Run("CX-05 between the cut and the step that follows, nothing of the old history is saved under the new one", func(t *testing.T) {
		m := savedChat(t)
		m.running, m.stepHistory = true, chat

		updated, _ := m.Update(contextCutMsg(chat))
		cut := updated.(model)

		if cut.stepHistory != nil || cut.saveConversation() != nil || cut.thread.id == m.thread.id || cut.thread.parent != m.thread.id {
			t.Errorf("step history %v thread %+v", cut.stepHistory, cut.thread)
		}
	})

	t.Run("CX-06 the conversation as it was can be resumed whole", func(t *testing.T) {
		m := savedChat(t)
		oldID := m.thread.id
		m.session.Agent.History = summarized
		updated, cmd := m.Update(contextCutMsg(chat))
		m = run(t, updated.(model), cmd)

		fresh := m
		fresh.session.Agent = m.session.Agent
		fresh = run(t, fresh, loadConversation(oldID))

		if len(fresh.session.Agent.History) != 6 || fresh.thread.id != oldID || fresh.thread.title != "list the files" {
			t.Errorf("history %d thread %+v", len(fresh.session.Agent.History), fresh.thread)
		}
	})
}
