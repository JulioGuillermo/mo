package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AL-01 | Happy    | The tool is web_fetch; the model is told no other name                    | P0       |
// | AL-02 | Happy    | The old name still runs it, directly and through tool_call               | P0       |
// | AL-03 | Edge     | Other names are left as they are                                         | P1       |

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestToolAliases(t *testing.T) {
	t.Run("AL-01 the tool is web_fetch; the model is told no other name", func(t *testing.T) {
		names := specNames(NewRegistry(newEnv(t), Options{Mode: ModeFull}).Specs(nil))

		if !contains(names, "web_fetch") || contains(names, "fetch") {
			t.Errorf("names = %v", names)
		}
	})

	t.Run("AL-02 the old name still runs it, directly and through tool_call", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Mode: ModeFull})

		direct, _, _ := registry.Unwrap(llm.ToolCall{Name: "tool_call", Args: json.RawMessage(`{"name":"fetch","args":{"url":"https://example.com"}}`)})
		_, isError := registry.Run(context.Background(), llm.ToolCall{Name: "fetch", Args: json.RawMessage(`{"url":"not a url"}`)})
		_, known := registry.find("fetch")

		if direct.Name != "web_fetch" || !known || !isError {
			t.Errorf("unwrapped to %q, known %v, error %v", direct.Name, known, isError)
		}
	})

	t.Run("AL-03 other names are left as they are", func(t *testing.T) {
		for _, name := range []string{"web_search", "read", "git", "web_fetch", ""} {
			if Canonical(name) != name {
				t.Errorf("%q became %q", name, Canonical(name))
			}
		}
		if Canonical("fetch") != "web_fetch" {
			t.Error("fetch did not become web_fetch")
		}
	})
}

func contains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
