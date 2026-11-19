package tools

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | BL-01 | Happy    | list gives each aside with its number, question, the start of its answer and what it did | P0 |
// | BL-02 | Happy    | read gives one whole; calls adds its tool calls and results; an unknown number errs | P0 |
// | BL-03 | Edge     | Without asides there is no btw_log tool; Restore puts back a conversation's    | P1       |
// | BL-04 | Happy    | The search of saved conversations finds what was asked and answered aside       | P0       |

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

func btwLog(t *testing.T, registry *Registry, fields map[string]any) (string, bool) {
	t.Helper()
	return registry.Run(context.Background(), llm.ToolCall{Name: BtwLogName, Args: args(t, fields)})
}

var cacheAside = sessions.Aside{Question: "why is the cache stale?", Answer: "It keeps the old key.\nRestart clears it.", History: []llm.Message{
	{Role: llm.RoleUser, Text: "why is the cache stale?"},
	{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "r1", Name: "read", Args: []byte(`{"path":"cache.go"}`)}}},
	{Role: llm.RoleTool, ToolCallID: "r1", Text: "func key() string { return old }"},
	{Role: llm.RoleAssistant, Text: "It keeps the old key.\nRestart clears it."},
}}

func TestBtwLog(t *testing.T) {
	t.Run("BL-01 list gives each aside with its number, question, the start of its answer and what it did", func(t *testing.T) {
		asides := NewAsides()
		registry := NewRegistry(Env{WorkDir: t.TempDir(), Asides: asides}, Options{})
		asides.Add(cacheAside)
		asides.Add(sessions.Aside{Bang: true, Question: "write notes", Answer: "Done.", Changes: []string{"write notes.txt"}})

		output, isError := btwLog(t, registry, map[string]any{"action": "list"})

		lines := strings.Split(output, "\n")
		if isError || len(lines) != 2 || lines[0] != "#1 /btw «why is the cache stale?» → It keeps the old key. Restart clears it." ||
			lines[1] != "#2 /btw! «write notes» → Done. · did: write notes.txt" {
			t.Errorf("output:\n%s", output)
		}
	})

	t.Run("BL-02 read gives one whole; calls adds its tool calls and results; an unknown number errs", func(t *testing.T) {
		asides := NewAsides()
		registry := NewRegistry(Env{WorkDir: t.TempDir(), Asides: asides}, Options{})
		asides.Add(cacheAside)

		plain, _ := btwLog(t, registry, map[string]any{"action": "read", "id": 1})
		withCalls, _ := btwLog(t, registry, map[string]any{"action": "read", "id": 1, "calls": true})
		missing, isError := btwLog(t, registry, map[string]any{"action": "read", "id": 7})

		if !strings.Contains(plain, "Answer: It keeps the old key.") || strings.Contains(plain, "cache.go") {
			t.Errorf("plain:\n%s", plain)
		}
		if !strings.Contains(withCalls, "[read") || !strings.Contains(withCalls, "return old") {
			t.Errorf("with calls:\n%s", withCalls)
		}
		if !isError || !strings.Contains(missing, "no side question 7") {
			t.Errorf("missing: %s", missing)
		}
	})

	t.Run("BL-03 without asides there is no btw_log tool; Restore puts back a conversation's", func(t *testing.T) {
		without := NewRegistry(Env{WorkDir: t.TempDir()}, Options{}).Specs(nil)
		asides := NewAsides()
		asides.Add(cacheAside)
		asides.Restore([]sessions.Aside{{ID: 4, Question: "old one"}})

		if slices.ContainsFunc(without, func(spec llm.ToolSpec) bool { return spec.Name == BtwLogName }) {
			t.Error("btw_log without asides")
		}
		if all := asides.All(); len(all) != 1 || all[0].ID != 4 || asides.Add(cacheAside) != 2 {
			t.Errorf("asides %+v", all)
		}
	})

	t.Run("BL-04 the search of saved conversations finds what was asked and answered aside", func(t *testing.T) {
		s := newRecallSetup(t)
		record := sessions.Record{Info: sessions.Info{ID: "work", WorkDir: workDirKey(s.env.WorkDir), Title: "work", Updated: day},
			History: []llm.Message{user("fix the build"), assistant("fixed")}, Asides: []sessions.Aside{cacheAside}}
		if err := sessions.Save(s.dir, record); err != nil {
			t.Fatal(err)
		}

		output, isError := s.run(t, map[string]any{"action": "search", "query": "stale cache"})

		if isError || !strings.Contains(output, "c:work#2") || !strings.Contains(output, "/btw why is the cache stale?") {
			t.Errorf("output:\n%s", output)
		}
	})
}
