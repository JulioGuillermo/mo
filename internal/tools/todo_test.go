package tools

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | TD-01 | Happy    | The list comes back marked, with how much is done                    | P0       |
// | TD-02 | Edge     | Common other names for a status are taken; unknown ones are refused  | P1       |
// | TD-03 | Happy    | The header names the progress and the task in progress               | P2       |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func todoArgs(t *testing.T, todos ...Todo) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTodo(t *testing.T) {
	registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{MaxOutput: 1000})

	t.Run("TD-01 the list comes back marked, with how much is done", func(t *testing.T) {
		args := todoArgs(t, Todo{"read the code", "completed"}, Todo{"write the tool", "in_progress"}, Todo{"test it", "pending"})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "todo", Args: args})

		if isError || output != "[x] read the code\n[>] write the tool\n[ ] test it\n1 of 3 done" {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("TD-02 common other names for a status are taken; unknown ones are refused", func(t *testing.T) {
		todos, err := ParseTodos(todoArgs(t, Todo{"a", "done"}, Todo{"b", "doing"}))
		_, refused := ParseTodos(todoArgs(t, Todo{"a", "maybe"}))
		_, empty := ParseTodos(todoArgs(t, Todo{" ", "pending"}))

		if err != nil || todos[0].Status != TodoCompleted || todos[1].Status != TodoInProgress || refused == nil || empty == nil {
			t.Errorf("todos = %+v err = %v refused = %v empty = %v", todos, err, refused, empty)
		}
	})

	t.Run("TD-03 the header names the progress and the task in progress", func(t *testing.T) {
		summary := registry.Summary(llm.ToolCall{Name: "todo", Args: todoArgs(t, Todo{"a", "completed"}, Todo{"b", "in_progress"})})

		if summary != "1/2 · b" || !strings.Contains(registry.Summary(llm.ToolCall{Name: "todo", Args: json.RawMessage(`{"todos":1}`)}), "invalid") {
			t.Errorf("summary = %q", summary)
		}
	})
}
