package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CC-01 | Happy    | /cache clear erases llama-server's slots and says how many                | P1       |
// | CC-02 | Edge     | With another server it says what clears the cache there; without clear, how to use it | P2 |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
)

// clearingProvider is llama-server as /cache clear sees it.
type clearingProvider struct {
	scriptedReplies
	cleared int
}

type scriptedReplies struct{}

func (scriptedReplies) Stream(context.Context, llm.Request, func(llm.Event)) (llm.Response, error) {
	return llm.Response{}, nil
}

func (c *clearingProvider) ClearSlots(context.Context) (int, error) {
	c.cleared++
	return 2, nil
}

func TestCacheClear(t *testing.T) {
	t.Run("CC-01 /cache clear erases llama-server's slots and says how many", func(t *testing.T) {
		m := conversationModel()
		provider := &clearingProvider{}
		m.session.Agent = &agent.Agent{Provider: provider}

		updated, cmd := m.runCommand("/cache clear")
		m = run(t, updated.(model), cmd)

		if provider.cleared != 1 || !strings.Contains(shown(m), "cache cleared: 2 slots") {
			t.Errorf("cleared %d transcript:\n%s", provider.cleared, shown(m))
		}
	})

	t.Run("CC-02 with another server it says what clears the cache there; without clear, how to use it", func(t *testing.T) {
		m := conversationModel()
		m.session.Agent = &agent.Agent{Provider: scriptedReplies{}}

		updated, cmd := m.runCommand("/cache clear")
		other := run(t, updated.(model), cmd)
		usage, _ := m.runCommand("/cache")

		if !strings.Contains(shown(other), "ollama stop") || !strings.Contains(shown(usage.(model)), "usage: /cache clear") {
			t.Errorf("other:\n%s\nusage:\n%s", shown(other), shown(usage.(model)))
		}
	})
}
