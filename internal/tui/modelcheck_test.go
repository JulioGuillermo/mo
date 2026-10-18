package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | MC-01 | Happy    | A model the server now has shows in the status line, with a note         | P0       |
// | MC-02 | Edge     | Only the window changed: the note says the context, the model stays      | P1       |
// | MC-03 | Happy    | A new conversation asks the server what it has before the first message  | P0       |
// | MC-04 | Edge     | With nothing to check, a new conversation asks nothing                   | P1       |
// | MC-05 | Happy    | Opening the models asks the server for them again, and what it has now   | P0       |
// | MC-07 | Happy    | A model no longer offered is told as a warning, in colour, not a dim note | P0      |
// | MC-06 | Happy    | The model marked in use in the list is the server's, not an old one      | P0       |

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
)

func TestModelCheck(t *testing.T) {
	t.Run("MC-01 a model the server now has shows in the status line, with a note", func(t *testing.T) {
		m := conversationModel()
		m.session.Model = "gemma.gguf"

		updated, _ := m.Update(modelMsg{model: "qwen.gguf", window: 131072})

		got := updated.(model)
		if got.session.Model != "qwen.gguf" || !strings.Contains(shown(got), "the server now has qwen") || !strings.Contains(shown(got), "131") {
			t.Errorf("model %q\n%s", got.session.Model, shown(got))
		}
	})

	t.Run("MC-02 only the window changed: the note says the context, the model stays", func(t *testing.T) {
		m := conversationModel()
		m.session.Model = "gemma.gguf"

		updated, _ := m.Update(modelMsg{model: "gemma.gguf", window: 16384})

		got := updated.(model)
		if got.session.Model != "gemma.gguf" || !strings.Contains(shown(got), "context is now") {
			t.Errorf("model %q\n%s", got.session.Model, shown(got))
		}
	})

	t.Run("MC-03 a new conversation asks the server what it has before the first message", func(t *testing.T) {
		m := conversationModel()
		m.session.Agent = &agent.Agent{Model: "old", Window: 4096, Refresh: func(context.Context, agent.CheckReason, string, int) agent.ModelCheck {
			return agent.ModelCheck{Model: "new", Window: 8192}
		}}

		cmd := m.checkModel()

		msg, ok := cmd().(modelMsg)
		if !ok || msg.model != "new" || msg.window != 8192 || m.session.Agent.ContextWindow() != 8192 {
			t.Errorf("msg = %+v ok = %v window = %d", msg, ok, m.session.Agent.ContextWindow())
		}
	})

	t.Run("MC-04 with nothing to check, a new conversation asks nothing", func(t *testing.T) {
		m := conversationModel()

		if cmd := m.checkModel(); cmd != nil {
			t.Error("a command was made")
		}
	})

	t.Run("MC-05 opening the models asks the server for them again, and what it has now", func(t *testing.T) {
		var lists atomic.Int32
		served := []string{"old.gguf"}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lists.Add(1)
			fmt.Fprintf(w, `{"data":[{"id":%q}]}`, served[0])
		}))
		t.Cleanup(server.Close)
		settings := config.ProviderConfig{Protocol: "openai", BaseURL: server.URL + "/v1"}
		current := &agent.Agent{Model: "old.gguf", Window: 8192, Refresh: func(context.Context, agent.CheckReason, string, int) agent.ModelCheck {
			return agent.ModelCheck{Model: served[0], Window: 8192}
		}}
		choice := modelChoice{provider: "llama-server", current: "old.gguf", check: current}

		first := loadModels(settings, choice)().(modelsMsg)
		served[0] = "new.gguf"
		second := loadModels(settings, choice)().(modelsMsg)

		if lists.Load() != 2 || first.models[0] != "old.gguf" || second.models[0] != "new.gguf" || first.followed != nil || second.followed == nil || second.followed.model != "new.gguf" {
			t.Errorf("lists %d first %+v second %+v", lists.Load(), first, second)
		}
	})

	t.Run("MC-06 the model marked in use in the list is the server's, not an old one", func(t *testing.T) {
		m := conversationModel()
		m.session.Model = "old.gguf"
		msg := modelsMsg{choice: modelChoice{provider: "llama-server", current: "old.gguf"}, models: []string{"new.gguf", "old.gguf"}, followed: &modelMsg{model: "new.gguf", window: 8192}}

		updated, _ := m.showModels(msg)

		got := updated.(model)
		view := ansi.Strip(got.settings.form.View())
		if got.session.Model != "new.gguf" || !strings.Contains(view, "new.gguf  ● in use") || strings.Contains(view, "old.gguf  ● in use") {
			t.Errorf("model %q\n%s", got.session.Model, view)
		}
	})

	t.Run("MC-07 a model no longer offered is told as a warning, in colour, not a dim note", func(t *testing.T) {
		m := conversationModel()
		m.session.Agent = &agent.Agent{Model: "gone", Window: 4096, Refresh: func(context.Context, agent.CheckReason, string, int) agent.ModelCheck {
			return agent.ModelCheck{Model: "gone", Window: 4096, Notice: "gone no longer appears in the models p offers"}
		}}

		printed, ok := m.checkModel()().(printMsg)

		warning, note := m.printer.Warning("x"), m.printer.Note("x")
		if !ok || !strings.Contains(string(printed), "no longer appears") || !strings.HasPrefix(ansi.Strip(string(printed)), noteMark) {
			t.Fatalf("printed %q ok %v", printed, ok)
		}
		if warning == note || !strings.Contains(warning, "224;175;104") || strings.Contains(note, "224;175;104") || !strings.Contains(string(printed), "224;175;104") {
			t.Errorf("warning %q note %q printed %q", warning, note, printed)
		}
	})
}
