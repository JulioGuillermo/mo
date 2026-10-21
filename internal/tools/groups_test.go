package tools

// Test Traceability Table
// | ID    | Category | Description                                                                   | Priority |
// | GR-01 | Happy    | Each tool has its own mode: full, compact without descriptions, or left to search | P0    |
// | GR-02 | Edge     | Search tools come with any tool compact or left to search, none with all full | P0       |
// | GR-06 | Happy    | tool_search tells what a compact tool does; found with declare it is declared whole | P0 |
// | GR-03 | Happy    | A group is one line in tool_list and named in tool_search's description       | P0       |
// | GR-04 | Happy    | tool_search with a group's name gives all its tools; more words filter them   | P0       |
// | GR-05 | Happy    | Each skill has its own mode: listed in full, by name, or left to skill_search | P1       |
// | GR-07 | Happy    | tool_search's description tells what each MCP server given says it is for     | P0       |

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func browserTools(count int) []Tool {
	var extra []Tool
	for i := range count {
		name := []string{"click", "type", "navigate", "snapshot", "close"}[i%5]
		extra = append(extra, Tool{
			Name:        "mcp__playwright__browser_" + name + strings.Repeat("_", i/5),
			Description: "Browser " + name,
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Run:         func(context.Context, Env, json.RawMessage) (string, error) { return "ok", nil },
		})
	}
	return extra
}

func groupedRegistry(t *testing.T) *Registry {
	t.Helper()
	extra := browserTools(10)
	groups := map[string]string{}
	for _, tool := range extra {
		groups[tool.Name] = "playwright"
	}
	return NewRegistry(newEnv(t), Options{Mode: ModeFull, Extra: extra, Groups: groups})
}

func TestToolModes(t *testing.T) {
	t.Run("GR-01 each tool has its own mode: full, compact without descriptions, or left to search", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Mode: ModeFull, Modes: map[string]Mode{"grep": ModeCompact, "git": ModeSearch}})
		specs := registry.Specs(nil)

		grep, _ := specOf(specs, "grep")
		read, _ := specOf(specs, "read")
		_, gitDeclared := specOf(specs, "git")
		listed := registry.list("")

		if grep.Description != "" || read.Description == "" || gitDeclared || !slices.Contains(specNames(specs), searchToolName) || listed != "git" {
			t.Errorf("grep %q read %q git declared %v listed %q", grep.Description, read.Description, gitDeclared, listed)
		}
	})

	t.Run("GR-02 search tools come with any tool compact or left to search, none with all full", func(t *testing.T) {
		full := specNames(NewRegistry(newEnv(t), Options{Mode: ModeFull}).Specs(nil))
		oneCompact := specNames(NewRegistry(newEnv(t), Options{Mode: ModeFull, Modes: map[string]Mode{"grep": ModeCompact}}).Specs(nil))

		if slices.ContainsFunc(full, isSearchTool) || !slices.Contains(oneCompact, searchToolName) {
			t.Errorf("full %v\none compact %v", full, oneCompact)
		}
	})

	t.Run("GR-06 tool_search tells what a compact tool does; found with declare it is declared whole", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Mode: ModeFull, Load: LoadDeclare, Modes: map[string]Mode{"grep": ModeCompact}})
		found := runSearch(t, registry, "select:grep")
		history := []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Name: searchToolName, Args: args(t, map[string]any{"query": "select:grep"})}}}}

		before, _ := specOf(registry.Specs(nil), "grep")
		after, _ := specOf(registry.Specs(history), "grep")

		if !strings.Contains(found, "grep: Search files for") || before.Description != "" || after.Description == "" {
			t.Errorf("found %q before %q after %q", found, before.Description, after.Description)
		}
		if listed := registry.list(""); strings.Contains(listed, "grep") {
			t.Errorf("a declared compact tool is listed: %q", listed)
		}
	})

	t.Run("GR-03 a group is one line in tool_list and named in tool_search's description", func(t *testing.T) {
		registry := groupedRegistry(t)
		specs := registry.Specs(nil)
		search, _ := specOf(specs, searchToolName)

		listed := registry.list("")

		if listed != "group playwright: 10 tools; tool_search playwright gives them" || !strings.Contains(search.Description, "Groups: playwright (10 tools)") {
			t.Errorf("listed %q\nsearch %q", listed, search.Description)
		}
		if slices.ContainsFunc(specNames(specs), func(name string) bool { return strings.HasPrefix(name, "mcp__") }) {
			t.Error("a grouped tool is declared")
		}
	})

	t.Run("GR-04 tool_search with a group's name gives all its tools; more words filter them", func(t *testing.T) {
		registry := groupedRegistry(t)

		all := runSearch(t, registry, "playwright")
		clicks := runSearch(t, registry, "mcp__playwright click")

		if strings.Count(all, "parameters:") != 10 {
			t.Errorf("all gave %d tools", strings.Count(all, "parameters:"))
		}
		if strings.Count(clicks, "parameters:") != 2 || !strings.Contains(clicks, "browser_click") || strings.Contains(clicks, "navigate") {
			t.Errorf("clicks:\n%s", clicks)
		}
	})

	t.Run("GR-05 each skill has its own mode: listed in full, by name, or left to skill_search", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Skills: someSkills(t), SkillModes: map[string]Mode{"pdf": ModeCompact, "deploy": ModeSearch}})
		specs := registry.Specs(nil)
		skill, _ := specOf(specs, skillToolName)

		found, _ := registry.Run(context.Background(), llm.ToolCall{Name: skillSearchToolName, Args: args(t, map[string]any{"query": "production"})})

		if !strings.Contains(skill.Description, "Skills: pdf") || strings.Contains(skill.Description, "deploy") || !strings.Contains(skill.Description, "skill_search") {
			t.Errorf("skill:\n%s", skill.Description)
		}
		if !strings.HasPrefix(found, "deploy:") {
			t.Errorf("found %q", found)
		}
	})

	t.Run("GR-07 tool_search's description tells what each MCP server given says it is for", func(t *testing.T) {
		extra := browserTools(2)
		groups := map[string]string{extra[0].Name: "playwright", extra[1].Name: "playwright"}
		descriptions := map[string]string{"playwright": "Drive a browser.", "jira": "Tickets."}
		registry := NewRegistry(newEnv(t), Options{Mode: ModeFull, Extra: extra, Groups: groups, ServerDescriptions: descriptions})

		search, _ := specOf(registry.Specs(nil), searchToolName)
		plain, _ := specOf(groupedRegistry(t).Specs(nil), searchToolName)

		if !strings.HasSuffix(search.Description, "What these MCP servers say they are for:\n- jira: Tickets.\n- playwright: Drive a browser.") {
			t.Errorf("description = %q", search.Description)
		}
		if strings.Contains(plain.Description, "say they are for") {
			t.Errorf("without descriptions: %q", plain.Description)
		}
	})
}
