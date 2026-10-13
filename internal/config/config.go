// Package config loads mo's settings: built-in defaults, then the user file
// (~/.config/mo/config.toml), then the project file (.mo.toml). The project
// file comes with the repository, so it can only change what is harmless.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/web"
)

type Config struct {
	Provider     string                    `toml:"provider,omitempty"`
	Model        string                    `toml:"model,omitempty"`
	SystemPrompt string                    `toml:"system_prompt,omitempty"`
	MaxTokens    int                       `toml:"max_tokens,omitzero"`
	Tools        Tools                     `toml:"tools,omitempty"`
	Context      Context                   `toml:"context,omitempty"`
	Memory       Memory                    `toml:"memory,omitempty"`
	Web          Web                       `toml:"web,omitempty"`
	MCP          map[string]MCPServer      `toml:"mcp,omitempty"`
	Render       Render                    `toml:"render,omitempty"`
	Providers    map[string]ProviderConfig `toml:"providers,omitempty"`
}

type Tools struct {
	Mode         tools.Mode        `toml:"mode,omitempty"`
	AlwaysLoaded []string          `toml:"always_loaded,omitempty"`
	Confirm      []string          `toml:"confirm,omitempty"`
	Disabled     []string          `toml:"disabled,omitempty"`
	MaxOutput    int               `toml:"max_output,omitzero"`
	Descriptions map[string]string `toml:"descriptions,omitempty"`
}

type Context struct {
	Window        int      `toml:"window,omitzero"`
	CompactAt     *float64 `toml:"compact_at,omitempty"`
	CompactPrompt string   `toml:"compact_prompt,omitempty"`
	Strategy      string   `toml:"strategy,omitempty"`
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
}

// Key returns the API key from the first of api_key, api_key_env and
// api_key_from that is set.
func (p ProviderConfig) Key() string {
	switch {
	case p.APIKey != "":
		return p.APIKey
	case p.APIKeyEnv != "":
		return os.Getenv(p.APIKeyEnv)
	case p.APIKeyFrom != "":
		return KeyFrom(p.APIKeyFrom)
	}
	return ""
}

func (p ProviderConfig) NeedsKey() bool {
	return p.APIKey != "" || p.APIKeyEnv != "" || p.APIKeyFrom != ""
}

func Defaults() Config {
	return Config{
		Tools: Tools{
			Mode:         tools.ModeFull,
			AlwaysLoaded: []string{"bash", "read", "edit"},
			Confirm:      []string{"bash", "rm", "fetch"},
			MaxOutput:    10000,
		},
		Context:   Context{CompactAt: ptr(0.75), Strategy: "compact"},
		Memory:    Memory{Dir: defaultMemoryDir()},
		Web:       Web{Search: web.SearchDuckDuckGo},
		Render:    Render{Images: render.ModeAuto},
		Providers: presets(),
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

func LogDir() string {
	return filepath.Join(dataHome(), "mo", "logs")
}

func ProjectFile(workDir string) string {
	return filepath.Join(workDir, ".mo.toml")
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
	imageModes := []render.Mode{render.ModeAuto, render.ModeKitty, render.ModeBlocks, render.ModeOff}
	if !slices.Contains(imageModes, cfg.Render.Images) {
		return fmt.Errorf("render.images %q: use auto, kitty, blocks or off", cfg.Render.Images)
	}
	if cfg.Context.Strategy != "compact" && cfg.Context.Strategy != "drop" {
		return fmt.Errorf("context.strategy %q: use compact or drop", cfg.Context.Strategy)
	}
	if compactAt := *cfg.Context.CompactAt; compactAt < 0 || compactAt >= 1 {
		return fmt.Errorf("context.compact_at %v: use a fraction from 0 (never) to below 1", compactAt)
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
	for name, provider := range layer.Providers {
		base.Providers[name] = mergeProvider(base.Providers[name], provider)
	}
	return base
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
	for name, provider := range layer.Providers {
		if known, ok := base.Providers[name]; ok {
			known.Model = override(known.Model, provider.Model)
			base.Providers[name] = known
		}
	}
	return base
}

// mergeHarmless applies the settings any layer may change.
func mergeHarmless(base, layer Config) Config {
	base.Model = override(base.Model, layer.Model)
	base.SystemPrompt = override(base.SystemPrompt, layer.SystemPrompt)
	base.MaxTokens = override(base.MaxTokens, layer.MaxTokens)
	base.Tools.Mode = override(base.Tools.Mode, layer.Tools.Mode)
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
	base.Render.Images = override(base.Render.Images, layer.Render.Images)
	return base
}

func mergeProvider(base, layer ProviderConfig) ProviderConfig {
	base.Protocol = override(base.Protocol, layer.Protocol)
	base.BaseURL = override(base.BaseURL, layer.BaseURL)
	base.APIKey = override(base.APIKey, layer.APIKey)
	base.APIKeyEnv = override(base.APIKeyEnv, layer.APIKeyEnv)
	base.APIKeyFrom = override(base.APIKeyFrom, layer.APIKeyFrom)
	base.Model = override(base.Model, layer.Model)
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
