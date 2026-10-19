package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | LT-01 | Happy    | A known limit ends the turn on the red band, the provider's error folded under it | P0 |
// | LT-02 | Happy    | A short limit that stayed says it was retried                             | P1       |
// | LT-03 | Edge     | A limit mo does not name shows as the provider sent it                    | P0       |

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func finishedWith(t *testing.T, code int, body string) model {
	t.Helper()
	m := conversationModel()
	m.session.Provider = "claude-code-detected"
	m.running, m.cancel = true, func() {}
	err := fmt.Errorf("turn: %w", &llm.HTTPError{Code: code, Status: fmt.Sprintf("%d %s", code, http.StatusText(code)), Header: http.Header{}, Body: body})
	updated, cmd := m.finishRun(err)
	return deliver(updated.(model), cmd, 3)
}

func TestLimitErrors(t *testing.T) {
	t.Run("LT-01 a known limit ends the turn on the red band, the provider's error folded under it", func(t *testing.T) {
		m := finishedWith(t, 429, `{"error":{"type":"usage_limit_reached","resets_in_seconds":600}}`)

		text := shown(m)
		if !strings.Contains(text, "⊘ claude-code-detected: usage limit reached · resets at") || !strings.Contains(text, "(in 10 min)") {
			t.Errorf("shown:\n%s", text)
		}
		if !strings.Contains(text, "✗ ") || strings.Contains(text, "resets_in_seconds") {
			t.Errorf("the provider's error should be there, folded:\n%s", text)
		}
	})

	t.Run("LT-02 a short limit that stayed says it was retried", func(t *testing.T) {
		m := finishedWith(t, 529, `{"type":"error","error":{"type":"overloaded_error"}}`)

		if text := shown(m); !strings.Contains(text, "⊘ claude-code-detected: overloaded · still so after 3 retries") {
			t.Errorf("shown:\n%s", text)
		}
	})

	t.Run("LT-03 a limit mo does not name shows as the provider sent it", func(t *testing.T) {
		m := finishedWith(t, 429, `slow down please`)

		if text := shown(m); strings.Contains(text, "⊘") || !strings.Contains(text, "✗ turn: 429 Too Many Requests: slow down please") {
			t.Errorf("shown:\n%s", text)
		}
	})
}
