package agent

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | NT-01 | Happy    | A note kept for the model reaches it with the next prompt, once       | P0       |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestNotes(t *testing.T) {
	t.Run("NT-01 a note kept for the model reaches it with the next prompt, once", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}, {Role: llm.RoleAssistant, Text: "ok"}}}
		a := newAgent(t, provider)
		a.Tell("The user deleted a task.")

		_ = a.Run(context.Background(), "go on", Hooks{})
		_ = a.Run(context.Background(), "and now", Hooks{})

		first, second := provider.requests[0].Messages, provider.requests[1].Messages
		if !strings.Contains(first[len(first)-1].Text, "The user deleted a task.") || strings.Count(second[len(second)-1].Text, "deleted a task") != 0 {
			t.Errorf("first %q second %q", first[len(first)-1].Text, second[len(second)-1].Text)
		}
	})
}
