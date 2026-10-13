package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Todo statuses, as models know them from other agents.
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
)

// Todo is one task of the model's list.
type Todo struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

func todoTool() Tool {
	return Tool{
		Name:        "todo",
		Description: "Your task list for work of several steps. Send the whole list each time; status pending, in_progress or completed; one in_progress at a time.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"todos":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["text","status"]}}},"required":["todos"]}`),
		Run:     runTodo,
		Summary: todoSummary,
	}
}

// ParseTodos reads the list a todo call sends. The list is the whole state:
// the latest call in a conversation is the list in force.
func ParseTodos(raw json.RawMessage) ([]Todo, error) {
	var args struct {
		Todos []Todo `json:"todos"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	for i, todo := range args.Todos {
		args.Todos[i].Text = strings.TrimSpace(todo.Text)
		switch todo.Status {
		case TodoPending, TodoInProgress, TodoCompleted:
		case "done", "complete":
			args.Todos[i].Status = TodoCompleted
		case "active", "doing", "in progress":
			args.Todos[i].Status = TodoInProgress
		default:
			return nil, fmt.Errorf("todo %d: status %q: use pending, in_progress or completed", i+1, todo.Status)
		}
		if args.Todos[i].Text == "" {
			return nil, fmt.Errorf("todo %d has no text", i+1)
		}
	}
	return args.Todos, nil
}

func runTodo(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
	todos, err := ParseTodos(raw)
	if err != nil {
		return "", err
	}
	if len(todos) == 0 {
		return "list cleared", nil
	}
	var out strings.Builder
	for _, todo := range todos {
		out.WriteString(map[string]string{TodoPending: "[ ] ", TodoInProgress: "[>] ", TodoCompleted: "[x] "}[todo.Status])
		out.WriteString(todo.Text)
		out.WriteByte('\n')
	}
	done, _ := TodoProgress(todos)
	if done == len(todos) {
		out.WriteString("all done")
	} else {
		fmt.Fprintf(&out, "%d of %d done", done, len(todos))
	}
	return out.String(), nil
}

// TodoProgress counts the completed tasks and names the one in progress.
func TodoProgress(todos []Todo) (done int, current string) {
	for _, todo := range todos {
		switch todo.Status {
		case TodoCompleted:
			done++
		case TodoInProgress:
			if current == "" {
				current = todo.Text
			}
		}
	}
	return done, current
}

func todoSummary(raw json.RawMessage) string {
	todos, err := ParseTodos(raw)
	if err != nil {
		return err.Error()
	}
	done, current := TodoProgress(todos)
	summary := fmt.Sprintf("%d/%d", done, len(todos))
	if current != "" {
		summary += " · " + current
	}
	return summary
}
