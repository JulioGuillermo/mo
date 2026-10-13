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

	"github.com/jg/mo/internal/tools"
)

type Config struct {
	Provider     string                    `toml:"provider"`
	Model        string                    `toml:"model"`
	SystemPrompt string                    `toml:"system_prompt"`
	MaxTokens    int                       `toml:"max_tokens"`
	Tools        Tools                     `toml:"tools"`
	Context      Context                   `toml:"context"`
	Memory       Memory                    `toml:"memory"`
	Providers    map[string]ProviderConfig `toml:"providers"`
}

type Tools struct {
	Mode         tools.Mode `toml:"mode"`
	AlwaysLoaded []string   `toml:"always_loaded"`
	Confirm      []string   `toml:"confirm"`
	Disabled     []string   `toml:"disabled"`
	MaxOutput    int        `toml:"max_output"`
}

type Context struct {
	Window        int      `toml:"window"`
	CompactAt     *float64 `toml:"compact_at"`
	CompactPrompt string   `toml:"compact_prompt"`
}

type Memory struct {
	Dir string `toml:"dir"`
}

type ProviderConfig struct {
	Protocol  string `toml:"type"`
	BaseURL   string `toml:"base_url"`
	APIKey    string `toml:"api_key"`
	APIKeyEnv string `toml:"api_key_env"`
	Model     string `toml:"model"`
}

func (p ProviderConfig) Key() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

func (p ProviderConfig) needsKey() bool {
	return p.APIKey != "" || p.APIKeyEnv != ""
}

func Defaults() Config {
	return Config{
		Tools: Tools{
			Mode:         tools.ModeFull,
			AlwaysLoaded: []string{"bash", "read", "edit"},
			Confirm:      []string{"bash", "rm"},
			MaxOutput:    10000,
		},
		Context:   Context{CompactAt: ptr(0.75)},
		Memory:    Memory{Dir: defaultMemoryDir()},
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
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "mo", "memory")
}

func ProjectFile(workDir string) string {
	return filepath.Join(workDir, ".mo.toml")
}

func Load(workDir string) (Config, error) {
	cfg := Defaults()
	user, err := readOptional(UserFile())
	if err != nil {
		return cfg, err
	}
	cfg = merge(cfg, user)
	project, err := readOptional(ProjectFile(workDir))
	if err != nil {
		return cfg, err
	}
	cfg = mergeProject(cfg, project)
	return cfg, validate(cfg)
}

func validate(cfg Config) error {
	modes := []tools.Mode{tools.ModeFull, tools.ModeCompact, tools.ModeSearch}
	if !slices.Contains(modes, cfg.Tools.Mode) {
		return fmt.Errorf("tools.mode %q: use full, compact or search", cfg.Tools.Mode)
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
	if chosen, ok := base.Providers[layer.Provider]; ok && !chosen.needsKey() {
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
	base.Context.Window = override(base.Context.Window, layer.Context.Window)
	if layer.Context.CompactAt != nil {
		base.Context.CompactAt = layer.Context.CompactAt
	}
	base.Context.CompactPrompt = override(base.Context.CompactPrompt, layer.Context.CompactPrompt)
	return base
}

func mergeProvider(base, layer ProviderConfig) ProviderConfig {
	base.Protocol = override(base.Protocol, layer.Protocol)
	base.BaseURL = override(base.BaseURL, layer.BaseURL)
	base.APIKey = override(base.APIKey, layer.APIKey)
	base.APIKeyEnv = override(base.APIKeyEnv, layer.APIKeyEnv)
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
