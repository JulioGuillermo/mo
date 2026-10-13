// Package tools holds the actions the model can take on the machine.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/web"
)

type Tool struct {
	Name             string
	Description      string
	Schema           json.RawMessage
	Run              func(ctx context.Context, env Env, args json.RawMessage) (string, error)
	Summary          func(args json.RawMessage) string
	ChangesPathArg   bool
	AlreadyConfirmed func(args json.RawMessage) bool
	LooseArgs        bool
}

const truncateHeadPercent = 60

type Env struct {
	WorkDir        string
	ProtectedPaths []string
	MemoryDir      string
}

// Resolve turns a path from the model into an absolute one, relative paths
// being taken from the working directory.
func (e Env) Resolve(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(e.WorkDir, path)
}

type Options struct {
	Mode         Mode
	AlwaysLoaded []string
	Disabled     []string
	MaxOutput    int
	Descriptions map[string]string
	Searcher     web.Searcher
	Extra        []Tool
}

type Registry struct {
	env     Env
	options Options
	tools   []Tool
	loaded  map[string]bool
}

func NewRegistry(env Env, options Options) *Registry {
	registry := &Registry{env: env, options: options, loaded: map[string]bool{}}
	for _, tool := range append(builtinsWith(options.Searcher), options.Extra...) {
		if slices.Contains(options.Disabled, tool.Name) {
			continue
		}
		if description, ok := options.Descriptions[tool.Name]; ok {
			tool.Description = description
		}
		registry.tools = append(registry.tools, tool)
	}
	if options.Mode == ModeSearch {
		registry.tools = append(registry.tools, searchTool(registry))
	}
	return registry
}

// BuiltinDescriptions maps every built-in tool to its default description.
func BuiltinDescriptions() map[string]string {
	descriptions := map[string]string{}
	for _, tool := range builtins() {
		descriptions[tool.Name] = tool.Description
	}
	return descriptions
}

func BuiltinNames() []string {
	var names []string
	for _, tool := range builtins() {
		names = append(names, tool.Name)
	}
	return names
}

func builtins() []Tool {
	return builtinsWith(nil)
}

func builtinsWith(searcher web.Searcher) []Tool {
	return []Tool{
		bashTool(), readTool(), writeTool(), editTool(), lsTool(), mkdirTool(), rmTool(), grepTool(),
		memoryTool(), fetchTool(), webSearchTool(searcher),
	}
}

func (r *Registry) Summary(call llm.ToolCall) string {
	tool, ok := r.find(call.Name)
	if !ok || tool.Summary == nil {
		return string(call.Args)
	}
	return tool.Summary(call.Args)
}

// AlreadyConfirmed tells whether a call repeats one that was approved, like
// reading on in a page that fetch already brought.
func (r *Registry) AlreadyConfirmed(call llm.ToolCall) bool {
	tool, ok := r.find(call.Name)
	return ok && tool.AlreadyConfirmed != nil && tool.AlreadyConfirmed(call.Args)
}

// Run executes a call and returns what goes back to the model, cut to the
// configured size. An error result is still text for the model to read.
func (r *Registry) Run(ctx context.Context, call llm.ToolCall) (output string, isError bool) {
	tool, ok := r.find(call.Name)
	if !ok {
		return fmt.Sprintf("unknown tool %q", call.Name), true
	}
	if err := checkArgNames(tool, call.Args); err != nil {
		return err.Error(), true
	}
	output, err := tool.Run(ctx, r.env, call.Args)
	if err != nil {
		output = strings.TrimSpace(output + "\n" + err.Error())
		isError = true
	}
	return Truncate(output, r.options.MaxOutput), isError
}

func (r *Registry) find(name string) (Tool, bool) {
	for _, tool := range r.tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

// Truncate keeps the head and the tail of a long text and says how much was
// dropped in between.
func Truncate(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	headSize := limit * truncateHeadPercent / 100
	tailSize := limit - headSize
	head := cutAtRune(text, headSize)
	tailStart := len(text) - tailSize
	for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
		tailStart++
	}
	tail := text[tailStart:]
	dropped := len(text) - len(head) - len(tail)
	return fmt.Sprintf("%s\n[... %d bytes cut ...]\n%s", head, dropped, tail)
}

// cutAtRune returns at most n bytes of text without splitting a character.
func cutAtRune(text string, n int) string {
	if n >= len(text) {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
}

func decodeArgs(raw json.RawMessage, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func argField(raw json.RawMessage, name string) string {
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return string(raw)
	}
	value, _ := fields[name].(string)
	return value
}
