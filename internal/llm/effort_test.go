package llm

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | EF-01 | Happy    | Anthropic gets the effort in output_config; without one, nothing      | P0       |
// | EF-02 | Happy    | OpenAI-compatible servers get it as reasoning_effort; without one, nothing | P0  |

import (
	"encoding/json"
	"strings"
	"testing"
)

func encoded(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEffort(t *testing.T) {
	t.Run("EF-01 Anthropic gets the effort in output_config; without one, nothing", func(t *testing.T) {
		with := encoded(t, buildAnthropicRequest(Request{Model: "m", Effort: "xhigh"}))
		without := encoded(t, buildAnthropicRequest(Request{Model: "m"}))

		if !strings.Contains(with, `"output_config":{"effort":"xhigh"}`) || strings.Contains(without, "output_config") {
			t.Errorf("with %s\nwithout %s", with, without)
		}
	})

	t.Run("EF-02 OpenAI-compatible servers get it as reasoning_effort; without one, nothing", func(t *testing.T) {
		provider := &OpenAI{}
		with := encoded(t, provider.buildRequest(Request{Model: "m", Effort: "low"}))
		without := encoded(t, provider.buildRequest(Request{Model: "m"}))

		if !strings.Contains(with, `"reasoning_effort":"low"`) || strings.Contains(without, "reasoning_effort") {
			t.Errorf("with %s\nwithout %s", with, without)
		}
	})
}
