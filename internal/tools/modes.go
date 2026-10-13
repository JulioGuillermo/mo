package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
)

// Mode decides how much of the tool list the model sees on every request:
// everything, everything without descriptions, or only tool_search plus the
// tools it has loaded so far.
type Mode string

const (
	ModeFull    Mode = "full"
	ModeCompact Mode = "compact"
	ModeSearch  Mode = "search"
)

const (
	searchToolName   = "tool_search"
	searchMaxResults = 5
)

func (r *Registry) Specs() []llm.ToolSpec {
	var specs []llm.ToolSpec
	for _, tool := range r.tools {
		if !r.visible(tool.Name) {
			continue
		}
		spec := llm.ToolSpec{Name: tool.Name, Description: tool.Description, Schema: tool.Schema}
		if tool.Name == searchToolName {
			spec.Description += " Not loaded yet: " + strings.Join(r.hiddenNames(), ", ")
		}
		if r.options.Mode == ModeCompact {
			spec.Description = ""
			spec.Schema = withoutDescriptions(tool.Schema)
		}
		specs = append(specs, spec)
	}
	return specs
}

func (r *Registry) visible(name string) bool {
	if r.options.Mode != ModeSearch || name == searchToolName {
		return true
	}
	return slices.Contains(r.options.AlwaysLoaded, name) || r.loaded[name]
}

func withoutDescriptions(schema json.RawMessage) json.RawMessage {
	var parsed struct {
		Type       string                    `json:"type"`
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required,omitempty"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return schema
	}
	for _, property := range parsed.Properties {
		delete(property, "description")
	}
	stripped, err := json.Marshal(parsed)
	if err != nil {
		return schema
	}
	return stripped
}

func searchTool(registry *Registry) Tool {
	return Tool{
		Name:        searchToolName,
		Description: "Find and load more tools. Query with keywords, or select:name1,name2. Loaded tools can be called right after.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
			var args struct {
				Query string `json:"query"`
			}
			if err := decodeArgs(raw, &args); err != nil {
				return "", err
			}
			return registry.search(args.Query), nil
		},
		Summary: func(args json.RawMessage) string { return argField(args, "query") },
	}
}

func (r *Registry) search(query string) string {
	found := r.match(query)
	if len(found) == 0 {
		return "no tools match; available: " + strings.Join(r.hiddenNames(), ", ")
	}
	var out strings.Builder
	out.WriteString("Loaded:\n")
	for _, tool := range found {
		r.loaded[tool.Name] = true
		fmt.Fprintf(&out, "- %s: %s\n", tool.Name, tool.Description)
	}
	return out.String()
}

func (r *Registry) match(query string) []Tool {
	if names, isSelect := strings.CutPrefix(query, "select:"); isSelect {
		var selected []Tool
		for _, name := range strings.Split(names, ",") {
			if tool, ok := r.find(strings.TrimSpace(name)); ok && tool.Name != searchToolName {
				selected = append(selected, tool)
			}
		}
		return selected
	}
	words := strings.Fields(strings.ToLower(query))
	type scored struct {
		tool  Tool
		score int
	}
	var ranked []scored
	for _, tool := range r.tools {
		if tool.Name == searchToolName {
			continue
		}
		text := strings.ToLower(tool.Name + " " + tool.Description)
		score := 0
		for _, word := range words {
			if strings.Contains(text, word) {
				score++
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{tool, score})
		}
	}
	slices.SortStableFunc(ranked, func(a, b scored) int { return b.score - a.score })
	var best []Tool
	for _, entry := range ranked[:min(len(ranked), searchMaxResults)] {
		best = append(best, entry.tool)
	}
	return best
}

func (r *Registry) hiddenNames() []string {
	var names []string
	for _, tool := range r.tools {
		if !r.visible(tool.Name) {
			names = append(names, tool.Name)
		}
	}
	return names
}
