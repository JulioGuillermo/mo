package tools

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | MD-01 | Happy    | full mode sends every tool with its description                    | P0       |
// | MD-02 | Happy    | compact mode drops tool and parameter descriptions, keeps schemas  | P0       |
// | MD-03 | Happy    | search mode declares the search tools and the always-loaded ones only | P0     |
// | MD-04 | Happy    | tool_search tells what the matching tools do and take                | P0       |
// | MD-05 | Happy    | context: the request never changes; declare: found tools are declared from history | P0 |
// | MD-06 | Happy    | tool_list names the tools, filtered, without descriptions             | P0       |
// | MD-07 | Happy    | tool_call stands for the tool it names; string args are taken         | P0       |
// | MD-09 | Error    | tool_call refuses unknown tools, search tools and args that are not JSON | P1     |
// | MD-08 | Happy    | a configured description replaces the built-in one, "" removes it  | P1       |

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func specNames(specs []llm.ToolSpec) []string {
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func searchRegistry(t *testing.T) *Registry {
	t.Helper()
	return NewRegistry(newEnv(t), Options{Mode: ModeSearch, AlwaysLoaded: []string{"read"}})
}

func runSearch(t *testing.T, registry *Registry, query string) string {
	t.Helper()
	output, isError := registry.Run(context.Background(), llm.ToolCall{Name: searchToolName, Args: args(t, map[string]any{"query": query})})
	if isError {
		t.Fatalf("tool_search failed: %s", output)
	}
	return output
}

func TestModes(t *testing.T) {
	t.Run("MD-01 full mode sends every tool with its description", func(t *testing.T) {
		specs := NewRegistry(newEnv(t), Options{Mode: ModeFull}).Specs(nil)

		if len(specs) != len(builtins()) {
			t.Fatalf("specs = %v", specNames(specs))
		}
		for _, spec := range specs {
			if spec.Description == "" {
				t.Errorf("%s has no description", spec.Name)
			}
		}
	})

	t.Run("MD-02 compact mode drops tool and parameter descriptions, keeps schemas", func(t *testing.T) {
		specs := NewRegistry(newEnv(t), Options{Mode: ModeCompact}).Specs(nil)

		for _, spec := range specs {
			if spec.Description != "" || strings.Contains(string(spec.Schema), "description") {
				t.Errorf("%s still describes itself: %q %s", spec.Name, spec.Description, spec.Schema)
			}
			var schema map[string]any
			if err := json.Unmarshal(spec.Schema, &schema); err != nil || schema["properties"] == nil {
				t.Errorf("%s lost its schema: %s", spec.Name, spec.Schema)
			}
		}
	})

	t.Run("MD-03 search mode declares the search tools and the always-loaded ones only", func(t *testing.T) {
		context := specNames(searchRegistry(t).Specs(nil))
		declare := specNames(NewRegistry(newEnv(t), Options{Mode: ModeSearch, Load: LoadDeclare, AlwaysLoaded: []string{"read"}}).Specs(nil))

		if strings.Join(context, ",") != "read,tool_list,tool_search,tool_call" || strings.Join(declare, ",") != "read,tool_list,tool_search" {
			t.Errorf("context = %v declare = %v", context, declare)
		}
	})
}

func searchCall(t *testing.T, query string) llm.Message {
	t.Helper()
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Name: searchToolName, Args: args(t, map[string]any{"query": query})}}}
}

func TestToolSearch(t *testing.T) {
	t.Run("MD-04 tool_search tells what the matching tools do and take", func(t *testing.T) {
		output := runSearch(t, searchRegistry(t), "select:grep")

		if !strings.HasPrefix(output, "grep: Search file contents") || !strings.Contains(output, `parameters: {"type":"object","properties":{"pattern"`) || !strings.Contains(output, "tool_call") {
			t.Errorf("output = %q", output)
		}
		for _, spec := range searchRegistry(t).Specs(nil) {
			if strings.Contains(spec.Description, "grep") {
				t.Errorf("%s names the hidden tools: %q", spec.Name, spec.Description)
			}
		}
	})

	t.Run("MD-05 context: the request never changes; declare: found tools are declared from history", func(t *testing.T) {
		history := []llm.Message{searchCall(t, "select:edit, write")}
		context := searchRegistry(t)
		declare := NewRegistry(newEnv(t), Options{Mode: ModeSearch, Load: LoadDeclare, AlwaysLoaded: []string{"read"}})

		if !slices.Equal(specNames(context.Specs(history)), specNames(context.Specs(nil))) {
			t.Errorf("context mode changed the request: %v", specNames(context.Specs(history)))
		}
		if strings.Join(specNames(declare.Specs(history)), ",") != "read,write,edit,tool_list,tool_search" || len(declare.Specs(nil)) != 3 {
			t.Errorf("declare = %v", specNames(declare.Specs(history)))
		}
	})

	t.Run("MD-06 tool_list names the tools, filtered, without descriptions", func(t *testing.T) {
		registry := searchRegistry(t)
		list := func(filter string) string {
			output, _ := registry.Run(context.Background(), llm.ToolCall{Name: listToolName, Args: args(t, map[string]any{"filter": filter})})
			return output
		}

		all, files, none := list(""), list("file"), list("kubernetes")

		if !strings.HasPrefix(all, "bash, read, write") || strings.Contains(all, "tool_") || strings.Contains(all, ":") {
			t.Errorf("all = %q", all)
		}
		if !strings.Contains(files, "read") || strings.Contains(files, "bash") || !strings.Contains(none, "no tools match") {
			t.Errorf("files = %q none = %q", files, none)
		}
	})

	t.Run("MD-07 tool_call stands for the tool it names; string args are taken", func(t *testing.T) {
		registry := searchRegistry(t)

		inner, wrapped, err := registry.Unwrap(llm.ToolCall{ID: "c", Name: callToolName, Args: json.RawMessage(`{"name":"grep","args":{"pattern":"x"}}`)})
		quoted, _, quotedErr := registry.Unwrap(llm.ToolCall{Name: callToolName, Args: json.RawMessage(`{"name":"ls","args":"{\"path\":\"src\"}"}`)})
		plain, plainWrapped, _ := registry.Unwrap(llm.ToolCall{Name: "read"})
		summary := registry.Summary(llm.ToolCall{Name: callToolName, Args: json.RawMessage(`{"name":"grep","args":{"pattern":"x"}}`)})

		if err != nil || !wrapped || inner.ID != "c" || inner.Name != "grep" || string(inner.Args) != `{"pattern":"x"}` {
			t.Errorf("inner = %+v err = %v", inner, err)
		}
		if quotedErr != nil || string(quoted.Args) != `{"path":"src"}` || plainWrapped || plain.Name != "read" || summary != "grep x" {
			t.Errorf("quoted = %+v plain = %+v summary = %q", quoted, plain, summary)
		}
	})

	t.Run("MD-09 tool_call refuses unknown tools, search tools and args that are not JSON", func(t *testing.T) {
		registry := searchRegistry(t)
		for _, raw := range []string{`{"name":"nope"}`, `{"name":"tool_call","args":{}}`, `{"name":"ls","args":"not json"}`} {
			if _, _, err := registry.Unwrap(llm.ToolCall{Name: callToolName, Args: json.RawMessage(raw)}); err == nil {
				t.Errorf("%s was taken", raw)
			}
		}
	})
}

func TestDescriptionOverrides(t *testing.T) {
	t.Run("MD-08 a configured description replaces the built-in one, an empty one removes it", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Mode: ModeFull, Descriptions: map[string]string{"bash": "Run bash.", "ls": ""}})

		descriptions := map[string]string{}
		for _, spec := range registry.Specs(nil) {
			descriptions[spec.Name] = spec.Description
		}

		if descriptions["bash"] != "Run bash." || descriptions["ls"] != "" || descriptions["read"] != BuiltinDescriptions()["read"] {
			t.Errorf("descriptions = %#v", descriptions)
		}
	})
}
