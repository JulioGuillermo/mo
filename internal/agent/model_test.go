package agent

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CM-01 | Happy    | A turn follows the model and window the server has, before it sends      | P0       |
// | CM-02 | Edge     | Nothing changed: no hook is told                                         | P0       |
// | CM-03 | Happy    | A model no longer offered is told by OnNotice, and the agent goes on     | P0       |
// | CM-04 | Happy    | /compact and /continue check the model too                               | P1       |
// | CM-05 | Edge     | A window that shrank is the one compaction is decided with               | P0       |
// | CM-06 | Edge     | An agent with no Refresh keeps its model and window                      | P1       |
// | CM-07 | Edge     | A zero model or window from Refresh keeps what the agent has             | P1       |
// | CM-08 | Happy    | A request that failed checks the model, and the error is still returned  | P0       |
// | CM-09 | Edge     | A stop by the user is not a failure to check                             | P1       |
// | CM-11 | Happy    | A warning goes to OnWarning when there is one, else to OnNotice          | P1       |
// | CM-10 | Edge     | Each moment says its reason: turn, compaction, new conversation          | P1       |

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func refreshing(check ModelCheck, calls *int) func(context.Context, CheckReason, string, int) ModelCheck {
	return func(_ context.Context, _ CheckReason, model string, window int) ModelCheck {
		*calls++
		return check
	}
}

func TestCheckModel(t *testing.T) {
	ctx := context.Background()
	done := llm.Message{Role: llm.RoleAssistant, Text: "ok"}

	t.Run("CM-01 a turn follows the model and window the server has, before it sends", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{done}}
		agent := newAgent(t, provider)
		agent.Model, agent.Window = "old", 8192
		calls := 0
		agent.Refresh = refreshing(ModelCheck{Model: "new", Window: 32768}, &calls)
		var told []string

		err := agent.Run(ctx, "hi", Hooks{OnModel: func(model string, window int) { told = append(told, model) }})

		if err != nil || calls != 1 || len(told) != 1 || told[0] != "new" {
			t.Fatalf("err = %v calls = %d told = %v", err, calls, told)
		}
		if provider.requests[0].Model != "new" || agent.ContextWindow() != 32768 {
			t.Errorf("sent to %q window %d", provider.requests[0].Model, agent.ContextWindow())
		}
	})

	t.Run("CM-02 nothing changed: no hook is told", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{done}})
		agent.Model, agent.Window = "same", 8192
		calls := 0
		agent.Refresh = refreshing(ModelCheck{Model: "same", Window: 8192}, &calls)
		told := 0

		_ = agent.Run(ctx, "hi", Hooks{OnModel: func(string, int) { told++ }})

		if calls != 1 || told != 0 {
			t.Errorf("calls = %d told = %d", calls, told)
		}
	})

	t.Run("CM-03 a model no longer offered is told by OnNotice, and the agent goes on", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{done}})
		agent.Model, agent.Window = "gone", 8192
		agent.Refresh = refreshing(ModelCheck{Model: "gone", Window: 8192, Notice: "gone is no longer offered"}, new(int))
		var notices []string

		err := agent.Run(ctx, "hi", Hooks{OnNotice: func(text string) { notices = append(notices, text) }})

		if err != nil || len(notices) != 1 || !strings.Contains(notices[0], "no longer offered") || agent.Model != "gone" {
			t.Errorf("err = %v notices = %v model = %q", err, notices, agent.Model)
		}
	})

	t.Run("CM-04 /compact and /continue check the model too", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "summary"}, done}})
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "a"}, {Role: llm.RoleAssistant, Text: "b"}}
		calls := 0
		agent.Refresh = refreshing(ModelCheck{}, &calls)

		_ = agent.CompactWith(ctx, Hooks{}, "")
		compacted := calls
		_ = agent.Continue(ctx, Hooks{})

		if compacted != 1 || calls != 2 {
			t.Errorf("after compact %d, after continue %d", compacted, calls)
		}
	})

	t.Run("CM-05 a window that shrank is the one compaction is decided with", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "summary of it all"}, done}}
		agent := newAgent(t, provider)
		agent.Window, agent.CompactAt = 1_000_000, 0.5
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: strings.Repeat("old words ", 400)}, {Role: llm.RoleAssistant, Text: strings.Repeat("old reply ", 400)}}
		agent.Refresh = refreshing(ModelCheck{Window: 2000}, new(int))
		compacted := false

		err := agent.Run(ctx, "go on", Hooks{OnCompact: func(int, int) { compacted = true }})

		if err != nil || !compacted {
			t.Errorf("err = %v compacted = %v window = %d", err, compacted, agent.ContextWindow())
		}
	})

	t.Run("CM-06 an agent with no Refresh keeps its model and window", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{done}})
		agent.Model, agent.Window = "m", 4096

		_ = agent.Run(ctx, "hi", Hooks{})

		if agent.Model != "m" || agent.ContextWindow() != 4096 {
			t.Errorf("model %q window %d", agent.Model, agent.ContextWindow())
		}
	})

	t.Run("CM-07 a zero model or window from Refresh keeps what the agent has", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{done}})
		agent.Model, agent.Window = "m", 4096
		agent.Refresh = refreshing(ModelCheck{}, new(int))

		_ = agent.Run(ctx, "hi", Hooks{})

		if agent.Model != "m" || agent.ContextWindow() != 4096 {
			t.Errorf("model %q window %d", agent.Model, agent.ContextWindow())
		}
	})

	t.Run("CM-08 a request that failed checks the model, and the error is still returned", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{err: errors.New("model not found")})
		var reasons []CheckReason
		agent.Refresh = func(_ context.Context, reason CheckReason, model string, window int) ModelCheck {
			reasons = append(reasons, reason)
			return ModelCheck{Model: model, Window: window, Notice: "the model is gone"}
		}
		var notices []string

		err := agent.Run(ctx, "hi", Hooks{OnNotice: func(text string) { notices = append(notices, text) }})

		if err == nil || !slices.Equal(reasons, []CheckReason{CheckTurn, CheckFailure}) || len(notices) != 2 {
			t.Errorf("err = %v reasons = %v notices = %v", err, reasons, notices)
		}
	})

	t.Run("CM-09 a stop by the user is not a failure to check", func(t *testing.T) {
		stopped, cancel := context.WithCancel(ctx)
		cancel()
		agent := newAgent(t, &scriptedProvider{err: context.Canceled})
		var reasons []CheckReason
		agent.Refresh = func(_ context.Context, reason CheckReason, model string, window int) ModelCheck {
			reasons = append(reasons, reason)
			return ModelCheck{}
		}

		_ = agent.Run(stopped, "hi", Hooks{})

		if slices.Contains(reasons, CheckFailure) {
			t.Errorf("reasons = %v", reasons)
		}
	})

	t.Run("CM-10 each moment says its reason: turn, compaction, new conversation", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "summary"}}})
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "a"}, {Role: llm.RoleAssistant, Text: "b"}}
		var reasons []CheckReason
		agent.Refresh = func(_ context.Context, reason CheckReason, model string, window int) ModelCheck {
			reasons = append(reasons, reason)
			return ModelCheck{}
		}

		_ = agent.CompactWith(ctx, Hooks{}, "")
		agent.CheckModel(ctx, Hooks{}, CheckNew)

		if !slices.Equal(reasons, []CheckReason{CheckCompact, CheckNew}) {
			t.Errorf("reasons = %v", reasons)
		}
	})

	t.Run("CM-11 a warning goes to OnWarning when there is one, else to OnNotice", func(t *testing.T) {
		for _, withWarning := range []bool{true, false} {
			agent := newAgent(t, &scriptedProvider{replies: []llm.Message{done}})
			agent.Refresh = refreshing(ModelCheck{Notice: "the model is gone"}, new(int))
			var warnings, notices int
			hooks := Hooks{OnNotice: func(string) { notices++ }}
			if withWarning {
				hooks.OnWarning = func(string) { warnings++ }
			}

			_ = agent.Run(ctx, "hi", hooks)

			if (withWarning && (warnings != 1 || notices != 0)) || (!withWarning && notices != 1) {
				t.Errorf("with a warning hook %v: warnings %d notices %d", withWarning, warnings, notices)
			}
		}
	})
}
