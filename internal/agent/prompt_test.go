package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | PR-01 | Happy    | The system prompt holds the working directory and no memories         | P1       |

import (
	"strings"
	"testing"
)

func TestSystemPrompt(t *testing.T) {
	t.Run("PR-01 the system prompt holds the working directory and no memories", func(t *testing.T) {
		prompt := SystemPrompt("", "/work/app")

		if !strings.Contains(prompt, "Working directory: /work/app") || strings.Contains(strings.ToLower(prompt), "memor") {
			t.Errorf("prompt = %q", prompt)
		}
	})
}
