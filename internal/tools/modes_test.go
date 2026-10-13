package tools

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | MD-01 | Happy    | full mode sends every tool with its description                    | P0       |
// | MD-02 | Happy    | compact mode drops tool and parameter descriptions, keeps schemas  | P0       |
// | MD-03 | Happy    | search mode sends tool_search and the always-loaded tools only     | P0       |
// | MD-04 | Happy    | tool_search by keywords loads the matching tools                   | P0       |
// | MD-05 | Happy    | tool_search select: loads exactly the named tools                  | P1       |
// | MD-06 | Edge     | tool_search with no match lists what can still be loaded           | P2       |
// | MD-07 | Happy    | tool_search names the tools not loaded yet in its description      | P1       |
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
		specs := NewRegistry(newEnv(t), Options{Mode: ModeFull}).Specs()

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
		specs := NewRegistry(newEnv(t), Options{Mode: ModeCompact}).Specs()

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

	t.Run("MD-03 search mode sends tool_search and the always-loaded tools only", func(t *testing.T) {
		names := specNames(searchRegistry(t).Specs())

		if strings.Join(names, ",") != "read,"+searchToolName {
			t.Errorf("names = %v", names)
		}
	})
}

func TestToolSearch(t *testing.T) {
	t.Run("MD-04 tool_search by keywords loads the matching tools", func(t *testing.T) {
		registry := searchRegistry(t)

		output := runSearch(t, registry, "search file contents regexp")

		names := specNames(registry.Specs())
		if !strings.Contains(output, "grep") || !slices.Contains(names, "grep") {
			t.Errorf("output = %q names = %v", output, names)
		}
	})

	t.Run("MD-05 tool_search select: loads exactly the named tools", func(t *testing.T) {
		registry := searchRegistry(t)

		runSearch(t, registry, "select:edit, write")

		names := specNames(registry.Specs())
		if strings.Join(names, ",") != "read,write,edit,"+searchToolName {
			t.Errorf("names = %v", names)
		}
	})

	t.Run("MD-06 tool_search with no match lists what can still be loaded", func(t *testing.T) {
		output := runSearch(t, searchRegistry(t), "kubernetes")

		if !strings.Contains(output, "no tools match") || !strings.Contains(output, "bash") {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("MD-07 tool_search names the tools not loaded yet in its description", func(t *testing.T) {
		registry := searchRegistry(t)
		runSearch(t, registry, "select:bash")

		specs := registry.Specs()
		search := specs[len(specs)-1]

		if !strings.Contains(search.Description, "grep") || strings.Contains(search.Description, "bash,") {
			t.Errorf("description = %q", search.Description)
		}
	})
}

func TestDescriptionOverrides(t *testing.T) {
	t.Run("MD-08 a configured description replaces the built-in one, an empty one removes it", func(t *testing.T) {
		registry := NewRegistry(newEnv(t), Options{Mode: ModeFull, Descriptions: map[string]string{"bash": "Run bash.", "ls": ""}})

		descriptions := map[string]string{}
		for _, spec := range registry.Specs() {
			descriptions[spec.Name] = spec.Description
		}

		if descriptions["bash"] != "Run bash." || descriptions["ls"] != "" || descriptions["read"] != BuiltinDescriptions()["read"] {
			t.Errorf("descriptions = %#v", descriptions)
		}
	})
}
