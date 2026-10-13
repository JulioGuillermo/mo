package agent

// Test Traceability Table
// | ID    | Category | Description                                                            | Priority |
// | CP-01 | Happy    | Past compact_at the run compacts first and keeps the current turn      | P0       |
// | CP-02 | Happy    | A current turn too big to keep is summarized, its request kept         | P0       |
// | CP-03 | Happy    | Below compact_at nothing is compacted                                  | P1       |
// | CP-04 | Error    | An empty summary fails the compaction and keeps the history            | P1       |
// | CP-05 | Happy    | The summary request carries the custom prompt and ends asking for it   | P1       |
// | CP-06 | Edge     | Compacting an empty conversation does nothing                          | P2       |
// | CP-07 | Happy    | Messages added since the last response count toward compaction        | P0       |
// | CP-08 | Happy    | The summary request is a plain transcript without tools                | P0       |
// | CP-09 | Edge     | A compaction that cannot shrink enough is not retried every request    | P0       |
// | CP-10 | Error    | A failed auto compaction lets the turn go on and says so               | P0       |
// | CP-11 | Happy    | Without usage from the provider the size is estimated                  | P1       |
// | CP-12 | Edge     | Two full compactions in one turn do not nest their summaries           | P0       |
// | CP-13 | Error    | A full window compacts again even right after a failed attempt         | P0       |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func longHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Text: "fix the build"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "read", `{"path":"a.txt"}`)}},
		{Role: llm.RoleTool, ToolCallID: "c1", Text: strings.Repeat("old output ", 50)},
		{Role: llm.RoleAssistant, Text: "fixed"},
	}
}

func TestCompaction(t *testing.T) {
	t.Run("CP-01 past compact_at the run compacts first and keeps the current turn", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "- build fixed in a.txt"},
			{Role: llm.RoleAssistant, Text: "tests pass"},
		}}
		agent := newAgent(t, provider)
		agent.Window, agent.CompactAt = 1000, 0.75
		agent.History = longHistory()
		agent.LastUsage = llm.Usage{InputTokens: 800}
		agent.historyAtUsage = len(agent.History)
		var before, after int
		hooks := Hooks{OnCompact: func(b, a int) { before, after = b, a }}

		if err := agent.Run(context.Background(), "now run the tests", hooks); err != nil {
			t.Fatal(err)
		}

		if len(provider.requests) != 2 {
			t.Fatalf("requests = %d", len(provider.requests))
		}
		continued := provider.requests[1].Messages
		if len(continued) != 1 || !strings.Contains(continued[0].Text, "- build fixed in a.txt") || !strings.HasSuffix(continued[0].Text, "now run the tests") {
			t.Errorf("history after compaction = %+v", continued)
		}
		if before < 800 || after <= 0 || after >= before {
			t.Errorf("before = %d after = %d", before, after)
		}
	})

	t.Run("CP-02 a current turn too big to keep is summarized, its request kept", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "- summary"},
			{Role: llm.RoleAssistant, Text: "ok"},
		}}
		agent := newAgent(t, provider)
		agent.Window, agent.CompactAt = 100, 0.5
		agent.LastUsage = llm.Usage{InputTokens: 90}

		if err := agent.Run(context.Background(), strings.Repeat("huge prompt ", 100), Hooks{}); err != nil {
			t.Fatal(err)
		}

		continued := provider.requests[1].Messages
		if len(continued) != 1 || !strings.HasSuffix(continued[0].Text, continueWork) {
			t.Errorf("history after compaction = %+v", continued)
		}
		if !strings.Contains(continued[0].Text, currentRequestHeader+"huge prompt") {
			t.Errorf("the current request was lost: %q", continued[0].Text[:200])
		}
	})

	t.Run("CP-03 below compact_at nothing is compacted", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "ok"}}}
		agent := newAgent(t, provider)
		agent.Window, agent.CompactAt = 1000, 0.75
		agent.History = longHistory()
		agent.LastUsage = llm.Usage{InputTokens: 700}
		agent.historyAtUsage = len(agent.History)

		if err := agent.Run(context.Background(), "hi", Hooks{}); err != nil {
			t.Fatal(err)
		}

		if len(provider.requests) != 1 || len(provider.requests[0].Messages) != 5 {
			t.Errorf("requests = %d", len(provider.requests))
		}
	})

	t.Run("CP-04 an empty summary fails the compaction and keeps the history", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "  "}}}
		agent := newAgent(t, provider)
		agent.History = longHistory()

		err := agent.Compact(context.Background(), Hooks{})

		if err == nil || len(agent.History) != 4 {
			t.Errorf("err = %v history = %d", err, len(agent.History))
		}
	})

	t.Run("CP-05 the summary request carries the custom prompt and ends asking for it", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "- s"}}}
		agent := newAgent(t, provider)
		agent.History = longHistory()
		agent.CompactPrompt = "Summarize in Spanish."

		if err := agent.Compact(context.Background(), Hooks{}); err != nil {
			t.Fatal(err)
		}

		request := provider.requests[0]
		last := request.Messages[len(request.Messages)-1]
		if request.System != "Summarize in Spanish." || last.Role != llm.RoleUser || !strings.HasSuffix(last.Text, writeSummaryNow) {
			t.Errorf("system = %q last = %+v", request.System, last)
		}
	})

	t.Run("CP-06 compacting an empty conversation does nothing", func(t *testing.T) {
		provider := &scriptedProvider{}
		agent := newAgent(t, provider)

		if err := agent.Compact(context.Background(), Hooks{}); err != nil || len(provider.requests) != 0 {
			t.Errorf("err = %v requests = %d", err, len(provider.requests))
		}
	})
}

func TestCompactionTriggers(t *testing.T) {
	t.Run("CP-07 messages added since the last response count toward compaction", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{})
		agent.Window, agent.CompactAt = 1000, 0.75
		agent.History = longHistory()[:3]
		agent.LastUsage = llm.Usage{InputTokens: 600}
		agent.historyAtUsage = 2
		agent.History[2].Text = strings.Repeat("x", 800)

		if !agent.needsCompaction() {
			t.Errorf("used = %d, should compact", agent.ContextUsed())
		}
	})

	t.Run("CP-08 the summary request is a plain transcript without tools", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "- s"}}}
		agent := newAgent(t, provider)
		agent.History = longHistory()

		if err := agent.Compact(context.Background(), Hooks{}); err != nil {
			t.Fatal(err)
		}

		request := provider.requests[0]
		if len(request.Tools) != 0 || len(request.Messages) != 1 || request.Messages[0].Role != llm.RoleUser {
			t.Fatalf("request = %+v", request)
		}
		if !strings.Contains(request.Messages[0].Text, "Assistant called read") || !strings.Contains(request.Messages[0].Text, "Result: old output") {
			t.Errorf("transcript = %q", request.Messages[0].Text)
		}
	})

	t.Run("CP-09 a compaction that cannot shrink enough is not retried every request", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "- s"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("c1", "ls", `{}`)}},
			{Role: llm.RoleAssistant, Text: "done"},
		}}
		agent := newAgent(t, provider)
		agent.System = strings.Repeat("big system prompt ", 300)
		agent.Window, agent.CompactAt = 1000, 0.5

		if err := agent.Run(context.Background(), "go", Hooks{}); err != nil {
			t.Fatal(err)
		}

		if len(provider.requests) != 3 {
			t.Errorf("requests = %d, want one summary and two turns", len(provider.requests))
		}
	})

	t.Run("CP-10 a failed auto compaction lets the turn go on and says so", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: ""},
			{Role: llm.RoleAssistant, Text: "answer"},
		}}
		agent := newAgent(t, provider)
		agent.Window, agent.CompactAt = 1000, 0.5
		agent.History = longHistory()
		agent.LastUsage = llm.Usage{InputTokens: 900}
		agent.historyAtUsage = len(agent.History)
		var notice string

		err := agent.Run(context.Background(), "next", Hooks{OnNotice: func(text string) { notice = text }})

		if err != nil || !strings.Contains(notice, "could not compact") {
			t.Errorf("err = %v notice = %q", err, notice)
		}
		if last := agent.History[len(agent.History)-1]; last.Text != "answer" {
			t.Errorf("last = %+v", last)
		}
	})

	t.Run("CP-11 without usage from the provider the size is estimated", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{})
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: strings.Repeat("x", 4000)}}

		if used := agent.ContextUsed(); used < 1000 {
			t.Errorf("used = %d", used)
		}
	})
}

func TestRepeatedCompaction(t *testing.T) {
	t.Run("CP-12 two full compactions in one turn do not nest their summaries", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, Text: "- first summary"},
			{Role: llm.RoleAssistant, Text: "- second summary"},
		}}
		agent := newAgent(t, provider)
		agent.Window = 100
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: strings.Repeat("big ", 200)}}
		agent.currentRequest = "the original request"

		for range 2 {
			if err := agent.Compact(context.Background(), Hooks{}); err != nil {
				t.Fatal(err)
			}
		}

		text := agent.History[0].Text
		if strings.Count(text, summaryHeader) != 1 || strings.Count(text, currentRequestHeader) != 1 {
			t.Errorf("nested summaries: %q", text)
		}
		if !strings.Contains(text, "second summary") || !strings.Contains(text, "the original request") {
			t.Errorf("text = %q", text)
		}
	})

	t.Run("CP-13 a full window compacts again even right after a failed attempt", func(t *testing.T) {
		agent := newAgent(t, &scriptedProvider{})
		agent.Window, agent.CompactAt = 1000, 0.75
		agent.History = longHistory()
		agent.LastUsage = llm.Usage{InputTokens: 1000}
		agent.historyAtUsage = len(agent.History)
		agent.sizeAfterCompaction = 990

		if !agent.needsCompaction() {
			t.Error("a full window should always compact")
		}
	})
}
