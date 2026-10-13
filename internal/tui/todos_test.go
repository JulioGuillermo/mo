package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | TP-01 | Happy    | A todo call puts the list above the input; it goes once all is done  | P0       |
// | TP-02 | Happy    | Resume and rewind bring back the list as it was then; /clear drops it | P1      |
// | TP-03 | Edge     | A long list counts the finished tasks and shows what is left         | P2       |

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func todoCall(t *testing.T, todos ...tools.Todo) llm.ToolCall {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"todos": todos})
	return llm.ToolCall{ID: "t", Name: "todo", Args: raw}
}

func TestTodoPanel(t *testing.T) {
	t.Run("TP-01 a todo call puts the list above the input; it goes once all is done", func(t *testing.T) {
		m := conversationModel()
		working, _ := m.Update(toolStartMsg{call: todoCall(t, tools.Todo{Text: "parse", Status: "completed"}, tools.Todo{Text: "render", Status: "in_progress"}, tools.Todo{Text: "test", Status: "pending"})})
		finished, _ := working.(model).Update(toolStartMsg{call: todoCall(t, tools.Todo{Text: "parse", Status: "completed"})})

		bottom := ansi.Strip(working.(model).bottom())
		if !strings.Contains(bottom, "todo 1/3") || !strings.Contains(bottom, "✓ parse") || !strings.Contains(bottom, "◐ render") || !strings.Contains(bottom, "○ test") {
			t.Errorf("bottom:\n%s", bottom)
		}
		if strings.Contains(ansi.Strip(finished.(model).bottom()), "todo") {
			t.Error("a finished list stayed")
		}
	})

	t.Run("TP-02 resume and rewind bring back the list as it was then; /clear drops it", func(t *testing.T) {
		m := savedModel(t)
		history := []llm.Message{
			{Role: llm.RoleUser, Text: "build it"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{todoCall(t, tools.Todo{Text: "step one", Status: "in_progress"})}},
			{Role: llm.RoleTool, ToolCallID: "t", Text: "[>] step one"},
			{Role: llm.RoleUser, Text: "go on"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{todoCall(t, tools.Todo{Text: "step one", Status: "completed"}, tools.Todo{Text: "step two", Status: "in_progress"})}},
		}
		m.replay(history)
		resumed := len(m.todos)
		m.session.Agent.History = history
		rewound, _ := m.rewind(m.session.Agent.Prompts()[1])
		cleared, _ := rewound.(model).runCommand("/clear")

		if resumed != 2 || len(rewound.(model).todos) != 1 || rewound.(model).todos[0].Status != "in_progress" || cleared.(model).todos != nil {
			t.Errorf("resumed = %d rewound = %+v", resumed, rewound.(model).todos)
		}
	})

	t.Run("TP-03 a long list counts the finished tasks and shows what is left", func(t *testing.T) {
		m := conversationModel()
		var todos []tools.Todo
		for i := range 12 {
			status := "pending"
			if i < 5 {
				status = "completed"
			}
			todos = append(todos, tools.Todo{Text: fmt.Sprint("task ", i), Status: status})
		}
		updated, _ := m.Update(toolStartMsg{call: todoCall(t, todos...)})

		panel := ansi.Strip(updated.(model).todoPanel())

		if !strings.Contains(panel, "✓ 5 done") || strings.Contains(panel, "task 0") || !strings.Contains(panel, "task 11") || strings.Count(panel, "\n") > shownTodos+2 {
			t.Errorf("panel:\n%s", panel)
		}
	})
}
