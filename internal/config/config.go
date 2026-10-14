// Package config loads mo's settings: built-in defaults, then the user file
// (~/.config/mo/config.toml), then the project file (.mo/settings.toml). A
// project file may come with the repository, so it can only change what is
// harmless, unless the user saved it from mo and it is unchanged since.
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jg/mo/internal/notify"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/web"
)

type Config struct {
	Provider     string                    `toml:"provider,omitempty"`
	Model        string                    `toml:"model,omitempty"`
	SystemPrompt string                    `toml:"system_prompt,omitempty"`
	MaxTokens    int                       `toml:"max_tokens,omitzero"`
	Effort       string                    `toml:"effort,omitempty"`
	Tools        Tools                     `toml:"tools,omitempty"`
	Context      Context                   `toml:"context,omitempty"`
	Memory       Memory                    `toml:"memory,omitempty"`
	Web          Web                       `toml:"web,omitempty"`
	MCP          map[string]MCPServer      `toml:"mcp,omitempty"`
	Skills       Skills                    `toml:"skills,omitempty"`
	Instructions Instructions              `toml:"instructions,omitempty"`
	Render       Render                    `toml:"render,omitempty"`
	Loop         Loop                      `toml:"loop,omitempty"`
	Permissions  Permissions               `toml:"permissions,omitempty"`
	Notify       Notify                    `toml:"notify,omitempty"`
	Providers    map[string]ProviderConfig `toml:"providers,omitempty"`
}

type Tools struct {
	Mode         tools.Mode `toml:"mode,omitempty"`
	Load         tools.Load `toml:"load,omitempty"`
	AlwaysLoaded []string   `toml:"always_loaded,omitempty"`
	Confirm      []string   `toml:"confirm,omitempty"`
	Disabled     []string   `toml:"disabled,omitempty"`
	// MCPDisabled are MCP servers not started: set up in the user file,
	// turned off everywhere or in one project.
	MCPDisabled  []string          `toml:"mcp_disabled,omitempty"`
	MaxOutput    int               `toml:"max_output,omitzero"`
	Descriptions map[string]string `toml:"descriptions,omitempty"`
	// Modes set the mode of single tools over Mode: full, compact, or
	// search, found with tool_search.
	Modes map[string]tools.Mode `toml:"modes,omitempty"`
	// MCPModes set the mode of all the tools of an MCP server, or group:
	// one entry in tool_search for the server, which finds all its tools.
	MCPModes map[string]tools.Mode `toml:"mcp_modes,omitempty"`
}

// Instructions are files that tell the agent how to work, like AGENTS.md:
// the user's in ~/.config/mo, and the project's from the repository's root
// down to the working directory, added to the system prompt.
type Instructions struct {
	Enabled *bool    `toml:"enabled,omitempty"`
	Files   []string `toml:"files,omitempty"`
}

// Skills are folders with a SKILL.md the model may load. Mode shows them in
// full (names and descriptions), compact (names) or search (found with
// skill_list and skill_search). Dirs are more folders of skills besides
// ~/.config/mo/skills and the project's .mo/skills.
type Skills struct {
	Mode     tools.Mode `toml:"mode,omitempty"`
	Disabled []string   `toml:"disabled,omitempty"`
	Dirs     []string   `toml:"dirs,omitempty"`
	// Modes set the mode of single skills over Mode.
	Modes map[string]tools.Mode `toml:"modes,omitempty"`
}

type Context struct {
	Window        int      `toml:"window,omitzero"`
	CompactAt     *float64 `toml:"compact_at,omitempty"`
	CompactPrompt string   `toml:"compact_prompt,omitempty"`
	Strategy      string   `toml:"strategy,omitempty"`
	KeepReasoning *bool    `toml:"keep_reasoning,omitempty"`
}

// Notify says when mo calls the user back and how: at the end of a turn,
// when it asks for permission; by default only while the terminal is not
// focused.
type Notify struct {
	Finish   *bool  `toml:"finish,omitempty"`
	Approval *bool  `toml:"approval,omitempty"`
	Method   string `toml:"method,omitempty"`
	Focused  *bool  `toml:"focused,omitempty"`
}

// Permissions are the mode tool calls start in and the rules for shell
// commands and fetches. Projects holds rules that apply only in one
// working directory; it lives in the user file, never in a project's.
type Permissions struct {
	Mode     string                  `toml:"mode,omitempty"`
	Allow    []string                `toml:"allow,omitempty"`
	Deny     []string                `toml:"deny,omitempty"`
	Projects map[string]ProjectRules `toml:"projects,omitempty"`
}

// ProjectRules add to the user's rules in one project, and may pick another
// mode there.
type ProjectRules struct {
	Mode  string   `toml:"mode,omitempty"`
	Allow []string `toml:"allow,omitempty"`
	Deny  []string `toml:"deny,omitempty"`
}

// Efforts are the efforts mo sends: empty for the model's default, Anthropic's
// low to max, and OpenAI's none and minimal besides. A model that does not
// take the one set answers with an error.
var Efforts = []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"}

// DefaultAllow are the commands that only read and run without asking.
var DefaultAllow = []string{
	"ls *", "pwd", "cat *", "head *", "tail *", "wc *", "grep *", "rg *", "tree *", "file *", "stat *",
	"which *", "echo *", "git status *", "git diff *", "git log *", "git show *", "git branch",
}

type Loop struct {
	Enabled       *bool `toml:"enabled,omitempty"`
	Replies       int   `toml:"replies,omitzero"`
	Repeats       int   `toml:"repeats,omitzero"`
	StreamRepeats int   `toml:"stream_repeats,omitzero"`
	Warnings      *int  `toml:"warnings,omitempty"`
}

type Web struct {
	Search       string `toml:"search,omitempty"`
	SearchURL    string `toml:"search_url,omitempty"`
	SearchKey    string `toml:"search_key,omitempty"`
	SearchKeyEnv string `toml:"search_key_env,omitempty"`
}

func (w Web) Key() string {
	if w.SearchKey != "" {
		return w.SearchKey
	}
	return os.Getenv(w.SearchKeyEnv)
}

// MCPServer starts a server with command and args, or reaches one at url.
// Values in env and headers may use $VAR to read the environment.
type MCPServer struct {
	Command string            `toml:"command,omitempty"`
	Args    []string          `toml:"args,omitempty"`
	Env     map[string]string `toml:"env,omitempty"`
	URL     string            `toml:"url,omitempty"`
	Headers map[string]string `toml:"headers,omitempty"`
	Trust   bool              `toml:"trust,omitempty"`
}

type Render struct {
	Images render.Mode `toml:"images,omitempty"`
	// Background is the colour mo paints behind everything, as #rrggbb, so
	// it looks the same in any terminal; "terminal" keeps the terminal's.
	Background string `toml:"background,omitempty"`
	// Sidebar shows the side panel of tasks and background jobs when the
	// terminal is wide enough ("auto"), or never ("off").
	Sidebar string `toml:"sidebar,omitempty"`
	// Status draws the status line as powerline segments, which need a
	// font with their arrows, or "plain".
	Status string `toml:"status,omitempty"`
	// Icons are Nerd Font icons in the status line, or "text" for plain
	// characters.
	Icons string `toml:"icons,omitempty"`
}

type Memory struct {
	Dir string `toml:"dir,omitempty"`
}

type ProviderConfig struct {
	Protocol   string `toml:"type,omitempty"`
	BaseURL    string `toml:"base_url,omitempty"`
	APIKey     string `toml:"api_key,omitempty"`
	APIKeyEnv  string `toml:"api_key_env,omitempty"`
	APIKeyFrom string `toml:"api_key_from,omitempty"`
	Model      string `toml:"model,omitempty"`
	// OAuth signs in to the provider instead of a key: the token it gives
	// is sent as the bearer and renewed when it expires.
	OAuth *OAuth `toml:"oauth,omitempty"`
}

// OAuth flows a provider may sign in with.
const (
	// OAuthDevice shows a code to enter on a page, for any terminal.
	OAuthDevice = "device"
	// OAuthBrowser opens the browser and takes the code on a local port,
	// with PKCE.
	OAuthBrowser = "browser"
	// OAuthClient takes a token for the client itself, with no user.
	OAuthClient = "client_credentials"
)

// OAuth is how to sign in to a provider with OAuth 2.
type OAuth struct {
	Flow            string            `toml:"flow,omitempty"`
	ClientID        string            `toml:"client_id,omitempty"`
	ClientSecret    string            `toml:"client_secret,omitempty"`
	ClientSecretEnv string            `toml:"client_secret_env,omitempty"`
	AuthURL         string            `toml:"auth_url,omitempty"`
	TokenURL        string            `toml:"token_url,omitempty"`
	DeviceURL       string            `toml:"device_url,omitempty"`
	Scopes          []string          `toml:"scopes,omitempty"`
	Params          map[string]string `toml:"params,omitempty"`
	// RedirectURL is the loopback address the browser flow listens on, when
	// the client only accepts one registered address; empty: any free port.
	RedirectURL string `toml:"redirect_url,omitempty"`
	// JSON says the token endpoint takes its requests and gives its answers
	// as JSON, not as a form.
	JSON bool `toml:"json,omitempty"`
}

// Secret is the client secret, written or from its variable.
func (o OAuth) Secret() string {
	if o.ClientSecret != "" {
		return o.ClientSecret
	}
	return os.Getenv(o.ClientSecretEnv)
}

// Key returns the API key from the first of api_key, api_key_from and
// api_key_env that is set.
func (p ProviderConfig) Key() string {
	switch {
	case p.APIKey != "":
		return p.APIKey
	case p.APIKeyFrom != "":
		return KeyFrom(p.APIKeyFrom)
	case p.APIKeyEnv != "":
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

// NeedsKey tells whether the provider takes a credential: a key, or a
// sign-in.
func (p ProviderConfig) NeedsKey() bool {
	return p.APIKey != "" || p.APIKeyEnv != "" || p.APIKeyFrom != "" || p.OAuth != nil
}

func Defaults() Config {
	return Config{
		Tools: Tools{
			Mode:         tools.ModeFull,
			Load:         tools.LoadContext,
			AlwaysLoaded: []string{"read", "edit", "write", "bash", "ask", "recall"},
			Confirm:      []string{"bash", "rm", "fetch"},
			MaxOutput:    10000,
		},
		Context:      Context{CompactAt: ptr(0.75), Strategy: "compact", KeepReasoning: ptr(false)},
		Memory:       Memory{Dir: defaultMemoryDir()},
		Skills:       Skills{Mode: tools.ModeFull},
		Instructions: Instructions{Enabled: ptr(true), Files: []string{"AGENTS.md", "AGENT.md"}},
		Web:          Web{Search: web.SearchDuckDuckGo},
		Render:       Render{Images: render.ModeAuto, Background: "#00081a", Sidebar: "auto", Status: "powerline", Icons: "nerd"},
		Loop:         Loop{Enabled: ptr(true), Replies: 10, Repeats: 3, StreamRepeats: 5, Warnings: ptr(2)},
		Permissions:  Permissions{Mode: "edits", Allow: DefaultAllow},
		Notify:       Notify{Finish: ptr(true), Approval: ptr(true), Method: "auto", Focused: ptr(false)},
		Providers:    presets(),
	}
}

func UserFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mo", "config.toml")
}

func defaultMemoryDir() string {
	return filepath.Join(dataHome(), "mo", "memory")
}

func PluginDir() string {
	return filepath.Join(ConfigHome(), "mo", "plugins")
}

func LogDir() string {
	return filepath.Join(dataHome(), "mo", "logs")
}

func Load(workDir string) (Config, error) {
	user, err := readOptional(UserFile())
	if err != nil {
		return Defaults(), err
	}
	return LoadWithUserLayer(workDir, user)
}

func validate(cfg Config) error {
	modes := []tools.Mode{tools.ModeFull, tools.ModeCompact, tools.ModeSearch}
	if !slices.Contains(modes, cfg.Tools.Mode) {
		return fmt.Errorf("tools.mode %q: use full, compact or search", cfg.Tools.Mode)
	}
	if !slices.Contains(Efforts, cfg.Effort) {
		return fmt.Errorf("effort %q: use %s, or leave it empty for the model's default", cfg.Effort, strings.Join(Efforts[1:], ", "))
	}
	if !slices.Contains(modes, cfg.Skills.Mode) {
		return fmt.Errorf("skills.mode %q: use full, compact or search", cfg.Skills.Mode)
	}
	for _, each := range []struct {
		name  string
		modes map[string]tools.Mode
		group bool
	}{{"tools.modes", cfg.Tools.Modes, false}, {"skills.modes", cfg.Skills.Modes, false}, {"tools.mcp_modes", cfg.Tools.MCPModes, true}} {
		for name, mode := range each.modes {
			if !slices.Contains(modes, mode) && !(each.group && mode == tools.ModeGroup) {
				return fmt.Errorf("%s.%s %q: use full, compact or search%s", each.name, name, mode, map[bool]string{true: ", or group"}[each.group])
			}
		}
	}
	if cfg.Tools.Load != tools.LoadContext && cfg.Tools.Load != tools.LoadDeclare {
		return fmt.Errorf("tools.load %q: use context or declare", cfg.Tools.Load)
	}
	imageModes := []render.Mode{render.ModeAuto, render.ModeKitty, render.ModeBlocks, render.ModeOff}
	if !slices.Contains(imageModes, cfg.Render.Images) {
		return fmt.Errorf("render.images %q: use auto, kitty, blocks or off", cfg.Render.Images)
	}
	if cfg.Context.Strategy != "compact" && cfg.Context.Strategy != "drop" {
		return fmt.Errorf("context.strategy %q: use compact or drop", cfg.Context.Strategy)
	}
	if cfg.Render.Icons != "nerd" && cfg.Render.Icons != "text" {
		return fmt.Errorf("render.icons %q: use nerd or text", cfg.Render.Icons)
	}
	if cfg.Render.Status != "powerline" && cfg.Render.Status != "plain" {
		return fmt.Errorf("render.status %q: use powerline or plain", cfg.Render.Status)
	}
	if cfg.Render.Sidebar != "auto" && cfg.Render.Sidebar != "off" {
		return fmt.Errorf("render.sidebar %q: use auto or off", cfg.Render.Sidebar)
	}
	if !ValidBackground(cfg.Render.Background) {
		return fmt.Errorf("render.background %q: use #rrggbb or terminal", cfg.Render.Background)
	}
	if err := validatePermissions(cfg.Permissions); err != nil {
		return err
	}
	if !slices.Contains(notify.Methods, notify.Method(cfg.Notify.Method)) {
		return fmt.Errorf("notify.method %q: use auto, terminal, desktop, bell or off", cfg.Notify.Method)
	}
	if compactAt := *cfg.Context.CompactAt; compactAt < 0 || compactAt >= 1 {
		return fmt.Errorf("context.compact_at %v: use a fraction from 0 (never) to below 1", compactAt)
	}
	return validateLoop(cfg.Loop)
}

// ValidBackground accepts a #rrggbb colour or "terminal".
func ValidBackground(text string) bool {
	text = strings.TrimSpace(text)
	if text == "terminal" {
		return true
	}
	if len(text) != 7 || text[0] != '#' {
		return false
	}
	for _, digit := range strings.ToLower(text[1:]) {
		if !strings.ContainsRune("0123456789abcdef", digit) {
			return false
		}
	}
	return true
}

func validatePermissions(rules Permissions) error {
	if !permissions.Mode(rules.Mode).Valid() {
		return fmt.Errorf("permissions.mode %q: use ask, edits, auto or all", rules.Mode)
	}
	for _, text := range slices.Concat(rules.Allow, rules.Deny) {
		if err := permissions.Compile(text); err != nil {
			return fmt.Errorf("permissions: %w", err)
		}
	}
	return nil
}

func validateLoop(loop Loop) error {
	if loop.Replies < 1 {
		return fmt.Errorf("loop.replies %d: use 1 or more", loop.Replies)
	}
	if loop.Repeats < 2 {
		return fmt.Errorf("loop.repeats %d: use 2 or more", loop.Repeats)
	}
	if loop.StreamRepeats < 2 {
		return fmt.Errorf("loop.stream_repeats %d: use 2 or more", loop.StreamRepeats)
	}
	if *loop.Warnings < 0 {
		return fmt.Errorf("loop.warnings %d: use 0 or more", *loop.Warnings)
	}
	return nil
}

func readOptional(path string) (Config, error) {
	var layer Config
	if path == "" {
		return layer, nil
	}
	_, err := toml.DecodeFile(path, &layer)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return layer, fmt.Errorf("%s: %w", path, err)
	}
	return layer, nil
}

func merge(base, layer Config) Config {
	base = mergeHarmless(base, layer)
	base.Provider = override(base.Provider, layer.Provider)
	base.Memory.Dir = override(base.Memory.Dir, expandHome(layer.Memory.Dir))
	base.Web.Search = override(base.Web.Search, layer.Web.Search)
	base.Web.SearchURL = override(base.Web.SearchURL, layer.Web.SearchURL)
	base.Web.SearchKey = override(base.Web.SearchKey, layer.Web.SearchKey)
	base.Web.SearchKeyEnv = override(base.Web.SearchKeyEnv, layer.Web.SearchKeyEnv)
	for name, server := range layer.MCP {
		if base.MCP == nil {
			base.MCP = map[string]MCPServer{}
		}
		base.MCP[name] = server
	}
	if layer.Tools.Confirm != nil {
		base.Tools.Confirm = layer.Tools.Confirm
	}
	if layer.Tools.Disabled != nil {
		base.Tools.Disabled = layer.Tools.Disabled
	}
	if layer.Tools.MCPDisabled != nil {
		base.Tools.MCPDisabled = layer.Tools.MCPDisabled
	}
	if layer.Skills.Disabled != nil {
		base.Skills.Disabled = layer.Skills.Disabled
	}
	if layer.Skills.Dirs != nil {
		base.Skills.Dirs = nil
		for _, dir := range layer.Skills.Dirs {
			base.Skills.Dirs = append(base.Skills.Dirs, expandHome(dir))
		}
	}
	for name, provider := range layer.Providers {
		base.Providers[name] = mergeProvider(base.Providers[name], provider)
	}
	base.Permissions = mergePermissions(base.Permissions, layer.Permissions)
	base.Notify = mergeNotify(base.Notify, layer.Notify)
	return base
}

func mergeNotify(base, layer Notify) Notify {
	base.Method = override(base.Method, layer.Method)
	for _, field := range []struct{ base, layer **bool }{
		{&base.Finish, &layer.Finish}, {&base.Approval, &layer.Approval}, {&base.Focused, &layer.Focused},
	} {
		if *field.layer != nil {
			*field.base = *field.layer
		}
	}
	return base
}

func mergePermissions(base, layer Permissions) Permissions {
	base.Mode = override(base.Mode, layer.Mode)
	if layer.Allow != nil {
		base.Allow = layer.Allow
	}
	if layer.Deny != nil {
		base.Deny = layer.Deny
	}
	for dir, rules := range layer.Projects {
		if base.Projects == nil {
			base.Projects = map[string]ProjectRules{}
		}
		base.Projects[dir] = rules
	}
	return base
}

// ForProject is the mode and rules in force in workDir: the user's, with
// those the user set for that directory added.
func (p Permissions) ForProject(workDir string) Permissions {
	rules := p.Projects[ProjectKey(workDir)]
	p.Mode = override(p.Mode, rules.Mode)
	p.Allow = append(slices.Clone(p.Allow), rules.Allow...)
	p.Deny = append(slices.Clone(p.Deny), rules.Deny...)
	p.Projects = nil
	return p
}

// ProjectKey is how a working directory is named in permissions.projects.
func ProjectKey(workDir string) string {
	if absolute, err := filepath.Abs(workDir); err == nil {
		return absolute
	}
	return filepath.Clean(workDir)
}

// mergeProject applies a project file without letting it redirect requests,
// read other keys, spend a paid API, move the memory, skip a confirmation or
// bring back a disabled tool: besides the harmless settings it may pick a
// provider that needs no key, set models, and add tools to confirm or disable.
func mergeProject(base, layer Config) Config {
	base = mergeHarmless(base, layer)
	if chosen, ok := base.Providers[layer.Provider]; ok && !chosen.NeedsKey() {
		base.Provider = layer.Provider
	}
	base.Tools.Confirm = union(base.Tools.Confirm, layer.Tools.Confirm)
	base.Tools.Disabled = union(base.Tools.Disabled, layer.Tools.Disabled)
	base.Tools.MCPDisabled = union(base.Tools.MCPDisabled, layer.Tools.MCPDisabled)
	base.Skills.Disabled = union(base.Skills.Disabled, layer.Skills.Disabled)
	for name, provider := range layer.Providers {
		if known, ok := base.Providers[name]; ok {
			known.Model = override(known.Model, provider.Model)
			base.Providers[name] = known
		}
	}
	base.Permissions.Mode = string(permissions.Stricter(permissions.Mode(base.Permissions.Mode), permissions.Mode(layer.Permissions.Mode)))
	base.Permissions.Deny = union(base.Permissions.Deny, layer.Permissions.Deny)
	return base
}

// mergeHarmless applies the settings any layer may change.
func mergeHarmless(base, layer Config) Config {
	base.Model = override(base.Model, layer.Model)
	base.SystemPrompt = override(base.SystemPrompt, layer.SystemPrompt)
	base.MaxTokens = override(base.MaxTokens, layer.MaxTokens)
	base.Effort = override(base.Effort, layer.Effort)
	base.Tools.Mode = override(base.Tools.Mode, layer.Tools.Mode)
	base.Tools.Modes = mergeModes(base.Tools.Modes, layer.Tools.Modes)
	base.Tools.MCPModes = mergeModes(base.Tools.MCPModes, layer.Tools.MCPModes)
	base.Skills.Modes = mergeModes(base.Skills.Modes, layer.Skills.Modes)
	base.Tools.Load = override(base.Tools.Load, layer.Tools.Load)
	base.Skills.Mode = override(base.Skills.Mode, layer.Skills.Mode)
	if layer.Instructions.Enabled != nil {
		base.Instructions.Enabled = layer.Instructions.Enabled
	}
	if layer.Instructions.Files != nil {
		base.Instructions.Files = layer.Instructions.Files
	}
	base.Tools.MaxOutput = override(base.Tools.MaxOutput, layer.Tools.MaxOutput)
	if layer.Tools.AlwaysLoaded != nil {
		base.Tools.AlwaysLoaded = layer.Tools.AlwaysLoaded
	}
	for tool, description := range layer.Tools.Descriptions {
		if base.Tools.Descriptions == nil {
			base.Tools.Descriptions = map[string]string{}
		}
		base.Tools.Descriptions[tool] = description
	}
	base.Context.Window = override(base.Context.Window, layer.Context.Window)
	if layer.Context.CompactAt != nil {
		base.Context.CompactAt = layer.Context.CompactAt
	}
	base.Context.CompactPrompt = override(base.Context.CompactPrompt, layer.Context.CompactPrompt)
	base.Context.Strategy = override(base.Context.Strategy, layer.Context.Strategy)
	if layer.Context.KeepReasoning != nil {
		base.Context.KeepReasoning = layer.Context.KeepReasoning
	}
	base.Render.Images = override(base.Render.Images, layer.Render.Images)
	base.Render.Background = override(base.Render.Background, layer.Render.Background)
	base.Render.Sidebar = override(base.Render.Sidebar, layer.Render.Sidebar)
	base.Render.Status = override(base.Render.Status, layer.Render.Status)
	base.Render.Icons = override(base.Render.Icons, layer.Render.Icons)
	base.Loop = mergeLoop(base.Loop, layer.Loop)
	return base
}

// mergeModes sets the modes layer names over base's, one by one.
func mergeModes(base, layer map[string]tools.Mode) map[string]tools.Mode {
	if len(layer) == 0 {
		return base
	}
	merged := maps.Clone(base)
	if merged == nil {
		merged = map[string]tools.Mode{}
	}
	maps.Copy(merged, layer)
	return merged
}

func mergeLoop(base, layer Loop) Loop {
	if layer.Enabled != nil {
		base.Enabled = layer.Enabled
	}
	base.Replies = override(base.Replies, layer.Replies)
	base.Repeats = override(base.Repeats, layer.Repeats)
	base.StreamRepeats = override(base.StreamRepeats, layer.StreamRepeats)
	if layer.Warnings != nil {
		base.Warnings = layer.Warnings
	}
	return base
}

func mergeProvider(base, layer ProviderConfig) ProviderConfig {
	base.Protocol = override(base.Protocol, layer.Protocol)
	base.BaseURL = override(base.BaseURL, layer.BaseURL)
	base.APIKey = override(base.APIKey, layer.APIKey)
	base.APIKeyEnv = override(base.APIKeyEnv, layer.APIKeyEnv)
	base.APIKeyFrom = override(base.APIKeyFrom, layer.APIKeyFrom)
	base.Model = override(base.Model, layer.Model)
	if layer.OAuth != nil {
		base.OAuth = layer.OAuth
	}
	return base
}

func union(base, extra []string) []string {
	for _, item := range extra {
		if !slices.Contains(base, item) {
			base = append(base, item)
		}
	}
	return base
}

func expandHome(path string) string {
	rest, found := strings.CutPrefix(path, "~/")
	if !found {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}

func ptr[T any](value T) *T {
	return &value
}

func override[T comparable](current, next T) T {
	var zero T
	if next == zero {
		return current
	}
	return next
}

// ActiveMCP are the MCP servers to start: those set up and not turned off.
func (c Config) ActiveMCP() map[string]MCPServer {
	active := map[string]MCPServer{}
	for name, server := range c.MCP {
		if !slices.Contains(c.Tools.MCPDisabled, name) {
			active[name] = server
		}
	}
	return active
}

// SkillDirs are the folders skills are found in, the later winning a name
// found twice: the user's, those added in the settings, the project's.
func (c Config) SkillDirs(workDir string) []string {
	dirs := []string{filepath.Join(ConfigHome(), "mo", "skills")}
	dirs = append(dirs, c.Skills.Dirs...)
	return append(dirs, filepath.Join(ProjectDir(workDir), "skills"))
}

// InstructionFiles are the names of the instructions files to read, none
// when they are turned off.
func (c Config) InstructionFiles() []string {
	if c.Instructions.Enabled != nil && !*c.Instructions.Enabled {
		return nil
	}
	return c.Instructions.Files
}

// SessionDir is where conversations are saved to resume them.
func SessionDir() string {
	return filepath.Join(dataHome(), "mo", "sessions")
}
