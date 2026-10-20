package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AF-01 | Happy    | A type filled in is kept in the menu with every field, and saved with the rest | P0 |
// | AF-02 | Error    | A bad name, model or rule is not kept and the editor says why            | P1       |
// | AF-03 | Happy    | Deleting a type takes it out; the last one leaves no types               | P1       |
// | AF-04 | Happy    | Agents shows Types ›, which lists the types with their model and opens one | P0     |
// | AF-05 | Happy    | Saving the other settings keeps the types                                | P0       |
// | AF-06 | Happy    | The tools to tick are the session's a subagent can have, each MCP server whole, and what the type has | P1 |
// | AF-07 | Happy    | The side panel and a subagent's menu show its type and model             | P1       |
// | AF-08 | Happy    | Types in its description is off unless turned on, and only then written   | P1       |

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
)

func TestAgentTypesForm(t *testing.T) {
	t.Run("AF-01 a type filled in is kept in the menu with every field, and saved with the rest", func(t *testing.T) {
		m, menu := menuModel()
		draft := newAgentTypeDraft(newTypeChoice, config.AgentType{})
		draft.name, draft.description, draft.model, draft.effort = "reviewer", "reviews a diff", providerModel{"anthropic", "sonnet"}, "high"
		draft.prompt, draft.canStart, draft.tools, draft.skills = "Be strict.", true, []string{"read", "grep"}, []string{"go-style"}
		draft.permissions, draft.deny, draft.maxSteps = "ask", "git push *\n", "12"

		layer, err := draft.applyTo(menu.layer)
		menu.layer = layer
		saved, _ := menu.apply(menu.layer)

		want := config.AgentType{Description: "reviews a diff", Model: "anthropic/sonnet", Effort: "high", Prompt: "Be strict.", CanStart: true,
			Tools: []string{"read", "grep"}, Skills: []string{"go-style"}, Permissions: "ask", Deny: []string{"git push *"}, MaxSteps: 12}
		got := saved.Agents.Types["reviewer"]
		if err != nil || got.Description != want.Description || got.Model != want.Model || got.Effort != want.Effort || got.Prompt != want.Prompt ||
			!got.CanStart || !slices.Equal(got.Tools, want.Tools) || !slices.Equal(got.Skills, want.Skills) || got.Permissions != want.Permissions ||
			!slices.Equal(got.Deny, want.Deny) || got.MaxSteps != 12 || !menu.changed() {
			t.Errorf("err %v type %+v changed %v", err, got, menu.changed())
		}
		if _, ok := m.session.Config.Agents.Types["reviewer"]; ok {
			t.Error("the session changed before saving")
		}
	})

	t.Run("AF-02 a bad name, model or rule is not kept and the editor says why", func(t *testing.T) {
		for _, broken := range []*agentTypeDraft{
			{name: "Bad Name"},
			{name: "r", model: providerModel{provider: "anthropic"}},
			{name: "r", deny: "re:("},
			{name: "r", maxSteps: "0"},
		} {
			if _, err := broken.applyTo(config.Config{}); err == nil {
				t.Errorf("%+v was kept", broken)
			}
		}
		m, menu := menuModel()
		updated, _ := m.editAgentType(menu, newTypeChoice)
		e := updated.(model).settings.editor
		shown, _ := e.save(updated.(model))

		if view := ansi.Strip(shown.(model).settings.form.View()); !strings.Contains(view, "not kept") || len(menu.layer.Agents.Types) != 0 {
			t.Errorf("view:\n%s", view)
		}
	})

	t.Run("AF-03 deleting a type takes it out; the last one leaves no types", func(t *testing.T) {
		m, menu := menuModel()
		menu.layer.Agents.Types = map[string]config.AgentType{"reviewer": {}}
		updated, _ := m.editAgentType(menu, "reviewer")

		after, _ := updated.(model).settings.editor.remove(updated.(model))

		if menu.layer.Agents.Types != nil || !strings.Contains(ansi.Strip(after.(model).settings.form.View()), "type reviewer deleted") {
			t.Errorf("types %v", menu.layer.Agents.Types)
		}
	})

	t.Run("AF-04 Agents shows Types ›, which lists the types with their model and opens one", func(t *testing.T) {
		m, menu := menuModel()
		menu.layer.Agents.Types = map[string]config.AgentType{"reviewer": {Model: "anthropic/sonnet"}}

		group, _ := m.showGroup(menu, agentsGroup(), "")
		list, _ := m.openMenuChoice(menu, menuAgentTypes)
		opened, _ := m.editAgentType(menu, "reviewer")

		if view := ansi.Strip(group.(model).settings.form.View()); !strings.Contains(view, "Types ›") || !strings.Contains(view, "1 · add, edit, delete") {
			t.Errorf("group:\n%s", view)
		}
		if view := ansi.Strip(list.(model).settings.form.View()); !strings.Contains(view, "reviewer") || !strings.Contains(view, "anthropic/sonnet") || !strings.Contains(view, "+ add a type") {
			t.Errorf("list:\n%s", view)
		}
		if view := ansi.Strip(opened.(model).settings.form.View()); !strings.Contains(view, "Type reviewer") || !strings.Contains(view, "Permissions") {
			t.Errorf("opened:\n%s", view)
		}
	})

	t.Run("AF-05 saving the other settings keeps the types", func(t *testing.T) {
		draft := defaultDraft()
		draft.agentsDepth = "2"
		layer := config.Config{Agents: config.Agents{Types: map[string]config.AgentType{"reviewer": {Model: "a/b"}}}}

		saved, err := draft.applyTo(layer, nil)

		if err != nil || saved.Agents.Types["reviewer"].Model != "a/b" || saved.Agents.Depth != 2 {
			t.Errorf("err %v agents %+v", err, saved.Agents)
		}
	})

	t.Run("AF-06 the tools to tick are the session's a subagent can have, each MCP server whole, and what the type has", func(t *testing.T) {
		m, menu := menuModel()
		m.session.ToolNames = []string{"read", "bash", tools.AgentName, tools.TellParentName, "title", "ui", tools.PeersName, "mcp__gh__issues"}
		m.session.Config.MCP = map[string]config.MCPServer{"gh": {Command: "gh-mcp"}}

		names := typeToolOptions(m, menu, []string{"read", "old_tool"})

		for _, want := range []string{"read", "bash", "mcp__gh__issues", "mcp__gh__*", "old_tool"} {
			if !slices.Contains(names, want) {
				t.Errorf("no %s in %v", want, names)
			}
		}
		if slices.ContainsFunc(names, func(name string) bool {
			return slices.Contains([]string{tools.AgentName, tools.TellParentName, "title", "ui", tools.PeersName}, name)
		}) {
			t.Errorf("names %v", names)
		}
	})

	t.Run("AF-07 the side panel and a subagent's menu show its type and model", func(t *testing.T) {
		info := team.Info{ID: 1, Depth: 1, Name: "r", Type: "reviewer", State: team.Running, Provider: "lab", Model: "m1"}

		if agentKindAndState(info) != "reviewer · running" || typeLead(info) != "reviewer · " || modelTail(info) != " · lab/m1" {
			t.Errorf("%q %q %q", agentKindAndState(info), typeLead(info), modelTail(info))
		}
		plain := team.Info{State: team.Done}
		if agentKindAndState(plain) != "done" || typeLead(plain) != "" || modelTail(plain) != "" {
			t.Errorf("plain %q %q %q", agentKindAndState(plain), typeLead(plain), modelTail(plain))
		}
	})

	t.Run("AF-08 Types in its description is off unless turned on, and only then written", func(t *testing.T) {
		draft := defaultDraft()
		untouched, _ := draft.applyTo(config.Config{}, nil)
		draft.agentsListed = true

		listed, err := draft.applyTo(config.Config{}, nil)

		if untouched.Agents.ListTypes != nil || err != nil || listed.Agents.ListTypes == nil || !*listed.Agents.ListTypes || typesListed(false) != "off: action types lists them" {
			t.Errorf("untouched %v listed %v err %v", untouched.Agents.ListTypes, listed.Agents.ListTypes, err)
		}
	})
}
