package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jg/mo/internal/tools"
)

// Project is a project's settings file as read: what it sets, where it is,
// and whether the user saved it from mo and it has not changed since.
type Project struct {
	Layer   Config
	Path    string
	Trusted bool
	data    []byte
}

// ProjectDir is the folder of a project's mo files.
func ProjectDir(workDir string) string {
	return filepath.Join(workDir, ".mo")
}

// ProjectFile is where the project's settings are saved.
func ProjectFile(workDir string) string {
	return filepath.Join(ProjectDir(workDir), "settings.toml")
}

// LegacyProjectFile is where they were kept before .mo/, still read when
// .mo/settings.toml does not exist.
func LegacyProjectFile(workDir string) string {
	return filepath.Join(workDir, ".mo.toml")
}

func trustFile() string {
	return filepath.Join(dataHome(), "mo", "trusted-projects.json")
}

// ReadProject reads .mo/settings.toml, or the old .mo.toml without it.
func ReadProject(workDir string) (Project, error) {
	for _, path := range []string{ProjectFile(workDir), LegacyProjectFile(workDir)} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Project{}, fmt.Errorf("%s: %w", path, err)
		}
		var layer Config
		if _, err := toml.Decode(string(data), &layer); err != nil {
			return Project{Path: path}, fmt.Errorf("%s: %w", path, err)
		}
		return Project{Layer: layer, Path: path, Trusted: isTrusted(path, data), data: data}, nil
	}
	return Project{}, nil
}

// HeldBack tells whether the project file asks for more than applies: it
// is not trusted, and trusting it would change the settings.
func (p Project) HeldBack(workDir string, user Config) bool {
	if p.Path == "" || p.Trusted {
		return false
	}
	restricted, _ := combine(workDir, user, p)
	trusted := p
	trusted.Trusted = true
	full, _ := combine(workDir, user, trusted)
	return !reflect.DeepEqual(restricted, full)
}

// Digest tells one content of the file from another; empty when there is
// no file.
func (p Project) Digest() string {
	if p.Path == "" {
		return ""
	}
	return digest(p.data)
}

// Trust records the file as read as the user's own, so all of it applies
// until it changes again.
func (p *Project) Trust() error {
	if err := trust(p.Path, p.data); err != nil {
		return err
	}
	p.Trusted = true
	return nil
}

// HeldBackKeys are the settings the file sets that do not apply while it
// is not trusted, by their names in the file.
func (p Project) HeldBackKeys(workDir string, user Config) []string {
	if p.Path == "" || p.Trusted {
		return nil
	}
	restricted, _ := combine(workDir, user, p)
	trusted := p
	trusted.Trusted = true
	full, _ := combine(workDir, user, trusted)
	return differentKeys(reflect.ValueOf(restricted), reflect.ValueOf(full), "")
}

// differentKeys names the fields that differ, down into the sections.
func differentKeys(a, b reflect.Value, prefix string) []string {
	var keys []string
	for i := range a.NumField() {
		name, _, _ := strings.Cut(a.Type().Field(i).Tag.Get("toml"), ",")
		left, right := a.Field(i), b.Field(i)
		if reflect.DeepEqual(left.Interface(), right.Interface()) {
			continue
		}
		if left.Kind() == reflect.Struct {
			keys = append(keys, differentKeys(left, right, prefix+name+".")...)
			continue
		}
		keys = append(keys, prefix+name)
	}
	return keys
}

// SaveProject writes layer as .mo/settings.toml, or removes the file when
// layer sets nothing, and trusts what it wrote: the user made it.
func SaveProject(workDir string, layer Config) error {
	path := ProjectFile(workDir)
	fields := setFields(reflect.ValueOf(layer))
	if len(fields) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_ = os.Remove(ProjectDir(workDir))
		return nil
	}
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(fields); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return trust(path, encoded.Bytes())
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func trustKey(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func isTrusted(path string, data []byte) bool {
	hashes := map[string]string{}
	ReadJSON(trustFile(), &hashes)
	return hashes[trustKey(path)] == digest(data)
}

// trust records the file as the user saved it. Once it changes, by a pull
// or by hand, it is only trusted again when saved from mo.
func trust(path string, data []byte) error {
	hashes := map[string]string{}
	ReadJSON(trustFile(), &hashes)
	hashes[trustKey(path)] = digest(data)
	encoded, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(trustFile()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(trustFile(), encoded, 0o600)
}

// WithProject applies a project file the user trusts: besides what any
// project file may change, it picks any provider, even one with a key, and
// says which tools to confirm or disable, and which MCP servers and skills
// to turn off, instead of only adding to them.
// Provider addresses and keys, the web, MCP and the memory stay the user's.
func WithProject(base, layer Config) Config {
	base.Providers = cloneProviders(base.Providers)
	base = mergeProject(base, layer)
	base.Provider = override(base.Provider, layer.Provider)
	if layer.Tools.Confirm != nil {
		base.Tools.Confirm = slices.Clone(layer.Tools.Confirm)
	}
	if layer.Tools.Disabled != nil {
		base.Tools.Disabled = slices.Clone(layer.Tools.Disabled)
	}
	if layer.Tools.MCPDisabled != nil {
		base.Tools.MCPDisabled = slices.Clone(layer.Tools.MCPDisabled)
	}
	if layer.Skills.Disabled != nil {
		base.Skills.Disabled = slices.Clone(layer.Skills.Disabled)
	}
	return base
}

func cloneProviders(providers map[string]ProviderConfig) map[string]ProviderConfig {
	clone := make(map[string]ProviderConfig, len(providers))
	for name, settings := range providers {
		clone[name] = settings
	}
	return clone
}

// EffectiveModel is the model a config asks for: the top-level one, else
// the provider's.
func EffectiveModel(cfg Config) string {
	return override(cfg.Providers[cfg.Provider].Model, cfg.Model)
}

// ProjectKeys are the settings a project overrides from the settings
// screen, by their names in the file.
var ProjectKeys = []string{
	"provider", "model", "max_tokens", "effort", "system_prompt",
	"tools.mode", "tools.load", "tools.always_loaded", "tools.confirm", "tools.disabled", "tools.mcp_disabled", "tools.max_output", "tools.read_max_size",
	"skills.mode", "skills.disabled", "instructions.enabled", "instructions.files",
	"context.window", "context.compact_at", "context.compact_prompt", "context.strategy", "context.keep_reasoning",
	"loop.enabled", "loop.replies", "loop.repeats", "loop.stream_repeats", "loop.warnings", "loop.steps",
}

// projectField points at the field a project key names.
func projectField(cfg *Config, key string) reflect.Value {
	fields := map[string]any{
		"provider": &cfg.Provider, "model": &cfg.Model, "max_tokens": &cfg.MaxTokens, "effort": &cfg.Effort, "system_prompt": &cfg.SystemPrompt,
		"tools.mode": &cfg.Tools.Mode, "tools.load": &cfg.Tools.Load, "tools.always_loaded": &cfg.Tools.AlwaysLoaded,
		"tools.confirm": &cfg.Tools.Confirm, "tools.disabled": &cfg.Tools.Disabled, "tools.max_output": &cfg.Tools.MaxOutput, "tools.read_max_size": &cfg.Tools.ReadMaxSize,
		"tools.mcp_disabled": &cfg.Tools.MCPDisabled, "skills.mode": &cfg.Skills.Mode, "skills.disabled": &cfg.Skills.Disabled,
		"instructions.enabled": &cfg.Instructions.Enabled, "instructions.files": &cfg.Instructions.Files,
		"context.window": &cfg.Context.Window, "context.compact_at": &cfg.Context.CompactAt,
		"context.compact_prompt": &cfg.Context.CompactPrompt, "context.strategy": &cfg.Context.Strategy,
		"context.keep_reasoning": &cfg.Context.KeepReasoning,
		"loop.enabled":           &cfg.Loop.Enabled, "loop.replies": &cfg.Loop.Replies, "loop.repeats": &cfg.Loop.Repeats,
		"loop.stream_repeats": &cfg.Loop.StreamRepeats, "loop.warnings": &cfg.Loop.Warnings, "loop.steps": &cfg.Loop.Steps,
	}
	return reflect.ValueOf(fields[key]).Elem()
}

// modeMaps are the maps of modes a project sets entry by entry, under keys
// such as "tools.modes.read".
func modeMaps(cfg *Config) map[string]*map[string]tools.Mode {
	return map[string]*map[string]tools.Mode{
		"tools.modes.": &cfg.Tools.Modes, "tools.mcp_modes.": &cfg.Tools.MCPModes, "skills.modes.": &cfg.Skills.Modes,
	}
}

// ProjectSets tells which of ProjectKeys a project layer sets, and which
// entries of the maps of modes. A model set for one of the providers counts
// as the model.
func ProjectSets(layer Config) map[string]bool {
	set := map[string]bool{}
	for _, key := range ProjectKeys {
		if !projectField(&layer, key).IsZero() {
			set[key] = true
		}
	}
	for prefix, modes := range modeMaps(&layer) {
		for name := range *modes {
			set[prefix+name] = true
		}
	}
	for _, provider := range layer.Providers {
		if provider.Model != "" {
			set["model"] = true
		}
	}
	return set
}

// ProjectOverrides puts into a project layer the values target has for the
// keys set, and clears the other ProjectKeys; what else the layer holds is
// kept. The model is written as the top-level one, whatever provider it is.
func ProjectOverrides(target Config, set map[string]bool, into Config) Config {
	target.Model = EffectiveModel(target)
	into.Providers = nil
	for _, key := range ProjectKeys {
		field := projectField(&into, key)
		if !set[key] {
			field.Set(reflect.Zero(field.Type()))
			continue
		}
		value := projectField(&target, key)
		switch {
		case value.Kind() == reflect.Slice && value.IsNil():
			field.Set(reflect.MakeSlice(value.Type(), 0, 0))
		case value.Kind() == reflect.Slice:
			field.Set(reflect.AppendSlice(reflect.MakeSlice(value.Type(), 0, value.Len()), value))
		case value.Kind() == reflect.Pointer && !value.IsNil():
			copied := reflect.New(value.Type().Elem())
			copied.Elem().Set(value.Elem())
			field.Set(copied)
		default:
			field.Set(value)
		}
	}
	targetMaps := modeMaps(&target)
	for prefix, modes := range modeMaps(&into) {
		*modes = nil
		for key := range set {
			name, ok := strings.CutPrefix(key, prefix)
			if mode := (*targetMaps[prefix])[name]; ok && mode != "" {
				if *modes == nil {
					*modes = map[string]tools.Mode{}
				}
				(*modes)[name] = mode
			}
		}
	}
	return into
}
