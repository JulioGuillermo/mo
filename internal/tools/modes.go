package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	// ModeGroup is for the tools of an MCP server: found with tool_search
	// as a group under the server's name.
	ModeGroup Mode = "group"
)

const (
	searchToolName   = "tool_search"
	listToolName     = "tool_list"
	callToolName     = "tool_call"
	searchMaxResults = 5
)

// Load says what finding a tool with tool_search does in search mode.
type Load string

const (
	// LoadContext leaves the tools found as the search result only: the
	// model calls them through tool_call, and the request never changes.
	LoadContext Load = "context"
	// LoadDeclare declares the tools found from the next request on.
	LoadDeclare Load = "declare"
)

// Specs are the tools declared to the model: those in full mode, those in
// compact mode without descriptions, and the search tools when any tool is
// not in full mode, so what a compact one does can be looked up too; with
// LoadDeclare also the tools found by the tool_search calls in history,
// whole, so a resumed or rewound conversation declares what it had found
// and a new one starts clean.
func (r *Registry) Specs(history []llm.Message) []llm.ToolSpec {
	found := r.foundIn(history)
	var specs []llm.ToolSpec
	for _, tool := range r.tools {
		mode := r.modeOf(tool.Name)
		if mode == ModeSearch && !found[tool.Name] {
			continue
		}
		spec := llm.ToolSpec{Name: tool.Name, Description: tool.Description, Schema: tool.Schema}
		if mode == ModeCompact && !found[tool.Name] {
			spec.Description = ""
			spec.Schema = withoutDescriptions(tool.Schema)
		}
		specs = append(specs, spec)
	}
	return specs
}

// modeOf is a tool's mode: search for one in a group, else its own, else
// the default. In search mode the always-loaded tools are full; the search
// and skill tools always are.
func (r *Registry) modeOf(name string) Mode {
	if isMetaTool(name) {
		return ModeFull
	}
	if _, grouped := r.options.Groups[name]; grouped {
		return ModeSearch
	}
	if mode := r.options.Modes[name]; mode == ModeFull || mode == ModeCompact || mode == ModeSearch {
		return mode
	}
	switch {
	case r.options.Mode == ModeSearch && slices.Contains(r.options.AlwaysLoaded, name):
		return ModeFull
	case r.options.Mode == ModeCompact || r.options.Mode == ModeSearch:
		return r.options.Mode
	}
	return ModeFull
}

// searchable are the tools tool_search finds: those in search mode, and
// those in compact mode, declared without what they do.
func (r *Registry) searchable() []Tool {
	var found []Tool
	for _, tool := range r.tools {
		if mode := r.modeOf(tool.Name); mode == ModeSearch || mode == ModeCompact {
			found = append(found, tool)
		}
	}
	return found
}

// groups are the groups of tools, by name, with their tools.
func (r *Registry) groups() map[string][]Tool {
	groups := map[string][]Tool{}
	for _, tool := range r.tools {
		if group, ok := r.options.Groups[tool.Name]; ok {
			groups[group] = append(groups[group], tool)
		}
	}
	return groups
}

// groupNamed is the group a word of a query names: its name, or the name
// with group: or mcp__ before it.
func (r *Registry) groupNamed(word string) string {
	word = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(word), "group:"), "mcp__")
	for group := range r.groups() {
		if strings.ToLower(group) == word {
			return group
		}
	}
	return ""
}

func isSearchTool(name string) bool {
	return name == searchToolName || name == listToolName || name == callToolName
}

// foundIn names the tools the tool_search calls of a history found, when
// found tools are declared.
func (r *Registry) foundIn(history []llm.Message) map[string]bool {
	found := map[string]bool{}
	if r.options.Load != LoadDeclare {
		return found
	}
	for _, message := range history {
		for _, call := range message.ToolCalls {
			if call.Name == searchToolName {
				for _, tool := range r.match(argField(call.Args, "query")) {
					found[tool.Name] = true
				}
			}
		}
	}
	return found
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

// searchTools are the tools of search mode: tool_list names what there is,
// tool_search tells what some do and takes, and with LoadContext tool_call
// runs one. The groups are named in tool_search's description, so the
// model knows of them without listing.
func searchTools(registry *Registry) []Tool {
	searchDescription := "Get what tools do and their parameters: keywords, or select:name1,name2 for exact names."
	if groups := registry.groups(); len(groups) > 0 {
		var names []string
		for _, group := range slices.Sorted(maps.Keys(groups)) {
			names = append(names, fmt.Sprintf("%s (%d tools)", group, len(groups[group])))
		}
		searchDescription += " A group's name gives all its tools, more words after it filter them. Groups: " + strings.Join(names, ", ") + "."
	}
	tools := []Tool{
		{
			Name:        listToolName,
			Description: "List the names of the tools you can use besides the ones declared. filter: words to match in a tool's name or description.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"filter":{"type":"string"}}}`),
			Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
				return registry.list(argField(raw, "filter")), nil
			},
			Summary: func(args json.RawMessage) string { return argField(args, "filter") },
		},
		{
			Name:        searchToolName,
			Description: searchDescription,
			Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			Run: func(_ context.Context, _ Env, raw json.RawMessage) (string, error) {
				return registry.search(argField(raw, "query")), nil
			},
			Summary: func(args json.RawMessage) string { return argField(args, "query") },
		},
	}
	if registry.options.Load != LoadDeclare {
		tools = append(tools, Tool{
			Name:        callToolName,
			Description: "Call a tool found with tool_search: its name and its args object.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"args":{"type":"object"}},"required":["name"]}`),
			Run: func(context.Context, Env, json.RawMessage) (string, error) {
				return "", errors.New("tool_call runs through the agent")
			},
			Summary: func(args json.RawMessage) string {
				inner, _, err := registry.Unwrap(llm.ToolCall{Name: callToolName, Args: args})
				if err != nil {
					return string(args)
				}
				return inner.Name + " " + registry.Summary(inner)
			},
		})
	}
	return tools
}

// Unwrap returns the call a tool_call stands for, and whether it was one.
// Arguments sent as a JSON string are taken too, as some models send them.
func (r *Registry) Unwrap(call llm.ToolCall) (llm.ToolCall, bool, error) {
	if call.Name != callToolName {
		return call, false, nil
	}
	var args struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	}
	if err := decodeArgs(call.Args, &args); err != nil {
		return call, true, err
	}
	var quoted string
	if json.Unmarshal(args.Args, &quoted) == nil {
		args.Args = json.RawMessage(quoted)
	}
	if len(args.Args) == 0 || string(args.Args) == "null" {
		args.Args = json.RawMessage("{}")
	}
	if !json.Valid(args.Args) {
		return call, true, errors.New("tool_call: args must be a JSON object")
	}
	if _, ok := r.find(args.Name); !ok || isMetaTool(args.Name) {
		return call, true, fmt.Errorf("tool_call: no tool %q; tool_list names them", args.Name)
	}
	return llm.ToolCall{ID: call.ID, Name: args.Name, Args: args.Args}, true, nil
}

// list names the tools left to search, not declared, whose name or
// description hold every word of the filter, and the groups with such a tool, each
// once with how many tools it has.
func (r *Registry) list(filter string) string {
	words := strings.Fields(strings.ToLower(filter))
	var names []string
	groups := map[string]int{}
	for _, tool := range r.searchable() {
		if r.modeOf(tool.Name) == ModeCompact {
			continue
		}
		group, grouped := r.options.Groups[tool.Name]
		text := strings.ToLower(tool.Name + " " + tool.Description + " " + group)
		switch {
		case !containsAll(text, words):
		case grouped:
			groups[group]++
		default:
			names = append(names, tool.Name)
		}
	}
	var lines []string
	if len(names) > 0 {
		lines = append(lines, strings.Join(names, ", "))
	}
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		lines = append(lines, fmt.Sprintf("group %s: %d tools; tool_search %s gives them", group, groups[group], group))
	}
	if len(lines) == 0 {
		return "no tools match; tool_list without a filter names them all"
	}
	return strings.Join(lines, "\n")
}

func containsAll(text string, words []string) bool {
	for _, word := range words {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

// search tells what the matching tools do and take, and how to call them.
func (r *Registry) search(query string) string {
	found := r.match(query)
	if len(found) == 0 {
		return "no tools match; tool_list names them"
	}
	var out strings.Builder
	for _, tool := range found {
		fmt.Fprintf(&out, "%s: %s\nparameters: %s\n\n", tool.Name, tool.Description, compactJSON(tool.Schema))
	}
	if r.options.Load == LoadDeclare {
		out.WriteString("They are declared from your next step on; call them directly.")
	} else {
		out.WriteString(`Call them with tool_call: {"name": "<tool>", "args": {...}}.`)
	}
	return out.String()
}

func compactJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return string(raw)
	}
	return out.String()
}

func (r *Registry) match(query string) []Tool {
	if names, isSelect := strings.CutPrefix(strings.TrimSpace(query), "select:"); isSelect {
		var selected []Tool
		for _, name := range strings.Split(names, ",") {
			if tool, ok := r.find(strings.TrimSpace(name)); ok && !isMetaTool(tool.Name) {
				selected = append(selected, tool)
			}
		}
		return selected
	}
	words := strings.Fields(strings.ToLower(query))
	candidates := r.searchable()
	limit := searchMaxResults
	if len(words) > 0 {
		if group := r.groupNamed(words[0]); group != "" {
			candidates, words = r.groups()[group], words[1:]
			limit = len(candidates)
			if len(words) == 0 {
				return candidates
			}
		}
	}
	type scored struct {
		tool  Tool
		score int
	}
	var ranked []scored
	for _, tool := range candidates {
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
	for _, entry := range ranked[:min(len(ranked), limit)] {
		best = append(best, entry.tool)
	}
	return best
}
