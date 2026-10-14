package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | TC-01 | Happy    | A call's dot says what it does: blocking, background, output, change, read | P1 |

import (
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestToolKinds(t *testing.T) {
	t.Run("TC-01 a call's dot says what it does: blocking, background, output, change, read", func(t *testing.T) {
		gitChanges := func(call llm.ToolCall) bool { return call.Name == "git" && string(call.Args) == `{"action":"commit"}` }
		for _, c := range []struct {
			name, args string
			want       toolKind
		}{
			{"bash", `{"command":"make"}`, kindExec},
			{"bash", `{"command":"make","background":true}`, kindBackground},
			{"bash", `{"action":"output","id":3}`, kindOutput},
			{"bash", `{"action":"kill","id":3}`, kindExec},
			{"edit", `{"path":"a"}`, kindWrite},
			{"git", `{"action":"commit"}`, kindWrite},
			{"git", `{"action":"log"}`, kindRead},
			{"read", `{"path":"a"}`, kindRead},
		} {
			if got := toolKindOf(llm.ToolCall{Name: c.name, Args: []byte(c.args)}, gitChanges); got != c.want {
				t.Errorf("%s %s = %d, want %d", c.name, c.args, got, c.want)
			}
		}
	})
}
