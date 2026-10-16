// Package tools holds the actions the model can take on the machine.
package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/skills"
	"github.com/jg/mo/internal/web"
)

type Tool struct {
	Name           string
	Description    string
	Schema         json.RawMessage
	Run            func(ctx context.Context, env Env, args json.RawMessage) (string, error)
	Summary        func(args json.RawMessage) string
	ChangesPathArg bool
	// Changes tells, for a tool whose actions differ, whether a call changes
	// files; such calls ask in ask mode.
	Changes func(args json.RawMessage) bool
	// Warning names a risk that asks in every mode but all, as pushing
	// leaves the machine.
	Warning          func(args json.RawMessage) string
	AlreadyConfirmed func(args json.RawMessage) bool
	LooseArgs        bool
	// View tools take the view fields, offset, limit, col, width, match and
	// numbered, and return what they asked for whole: the model chose how
	// much. ViewLimit is the lines shown by default, ViewSelf a tool that
	// applies the view itself, from ViewFrom.
	View      bool
	ViewLimit int
	ViewSelf  bool
}

const truncateHeadPercent = 60

type Env struct {
	WorkDir        string
	ProtectedPaths []string
	MemoryDir      string
	// Jobs runs bash commands in the background; without it they cannot.
	Jobs *Jobs
	// Recall searches memories and past conversations.
	Recall *Recall
	// Titles renames the conversation; without it there is no title tool.
	Titles *Titles
	// UIs holds the interfaces the model draws; without it there is no ui
	// tool.
	UIs *UIs
	// MaxOutput is how much of a tool's output reaches the model, as the
	// registry cuts it; 0 for no limit.
	MaxOutput int
	// ReadMaxSize is the largest file read takes whole, in bytes; a larger
	// one only in parts. 0 for no limit.
	ReadMaxSize int64
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
	Load         Load
	AlwaysLoaded []string
	Disabled     []string
	MaxOutput    int
	ReadMaxSize  int64
	Descriptions map[string]string
	Searcher     web.Searcher
	Extra        []Tool
	// Modes set the mode of single tools over Mode.
	Modes map[string]Mode
	// Groups put tools in a group, by name: found together with tool_search
	// under the group's name, as the tools of an MCP server.
	Groups map[string]string
	// Skills are the skills the model may load, shown as SkillMode says,
	// or SkillModes for single ones; without any there are no skill tools.
	Skills     []skills.Skill
	SkillMode  Mode
	SkillModes map[string]Mode
}

type Registry struct {
	env     Env
	options Options
	tools   []Tool
}

func NewRegistry(env Env, options Options) *Registry {
	env.MaxOutput = options.MaxOutput
	env.ReadMaxSize = options.ReadMaxSize
	registry := &Registry{env: env, options: options}
	for _, tool := range append(builtinsWith(options.Searcher), options.Extra...) {
		if slices.Contains(options.Disabled, tool.Name) || (tool.Name == titleToolName && env.Titles == nil) || (tool.Name == "ui" && env.UIs == nil) {
			continue
		}
		if description, ok := options.Descriptions[tool.Name]; ok {
			tool.Description = description
		}
		if tool.View {
			tool.Schema = withViewSchema(tool.Schema)
		}
		registry.tools = append(registry.tools, tool)
	}
	if slices.ContainsFunc(registry.tools, func(tool Tool) bool { return registry.modeOf(tool.Name) != ModeFull }) {
		registry.tools = append(registry.tools, searchTools(registry)...)
	}
	registry.tools = append(registry.tools, skillTools(options.Skills, options.SkillMode, options.SkillModes)...)
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

// ReservedNames are the names no added tool may take: the built-in tools
// and the search and skill tools.
func ReservedNames() []string {
	return append(BuiltinNames(), searchToolName, listToolName, callToolName, skillToolName, skillListToolName, skillSearchToolName)
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
		memoryTool(), fetchTool(), webSearchTool(searcher), todoTool(), askTool(), gitTool(), titleTool(), uiTool(),
	}
}

// Recall is the search of memories and past conversations, or nil.
func (r *Registry) Recall() *Recall {
	if r == nil {
		return nil
	}
	return r.env.Recall
}

// Titles renames the conversation, or nil, also for no registry.
func (r *Registry) Titles() *Titles {
	if r == nil {
		return nil
	}
	return r.env.Titles
}

// UIs are the interfaces the model drew, or nil, also for no registry.
func (r *Registry) UIs() *UIs {
	if r == nil {
		return nil
	}
	return r.env.UIs
}

// Jobs are the session's background jobs, or nil, also for no registry.
func (r *Registry) Jobs() *Jobs {
	if r == nil {
		return nil
	}
	return r.env.Jobs
}

func (r *Registry) WorkDir() string {
	return r.env.WorkDir
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
	if !tool.View {
		output, err := tool.Run(ctx, r.env, call.Args)
		if err != nil {
			output = strings.TrimSpace(output + "\n" + err.Error())
			isError = true
		}
		return Truncate(output, r.options.MaxOutput), isError
	}
	view, err := parseView(call.Args, cmp.Or(tool.ViewLimit, viewDefaultLimit))
	if err != nil {
		return err.Error(), true
	}
	output, err = tool.Run(context.WithValue(ctx, viewKey{}, view), r.env, call.Args)
	if !tool.ViewSelf {
		output = view.Apply(output)
	}
	if err != nil {
		output = strings.TrimSpace(output + "\n" + err.Error())
		isError = true
	}
	return output, isError
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
