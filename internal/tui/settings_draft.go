package tui

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

// settingsDraft holds what the settings form edits, as plain strings and
// lists, starting from the defaults plus the user file.
type settingsDraft struct {
	provider      string
	model         string
	mode          string
	alwaysLoaded  []string
	confirm       []string
	disabled      []string
	maxOutput     string
	window        string
	compactAt     string
	strategy      string
	keepReasoning bool
	systemPrompt  string
	compactPrompt string
	maxTokens     string
	descriptions  map[string]*string

	defaultDescriptions map[string]string
	savedDescriptions   map[string]string

	search       string
	searchURL    string
	searchKeyEnv string
	searchKey    string
	savedKey     bool
	forgetKey    bool
	images       string
	memoryDir    string

	loopEnabled       bool
	loopReplies       string
	loopRepeats       string
	loopStreamRepeats string
	loopWarnings      string

	initialProvider string
	initialModel    string
}

func newSettingsDraft(layer config.Config) *settingsDraft {
	cfg := config.UserSettings(layer)
	model := layer.Model
	if model == "" {
		model = cfg.Providers[layer.Provider].Model
	}
	draft := &settingsDraft{
		provider:      layer.Provider,
		model:         model,
		mode:          string(cfg.Tools.Mode),
		alwaysLoaded:  slices.Clone(cfg.Tools.AlwaysLoaded),
		confirm:       slices.Clone(cfg.Tools.Confirm),
		disabled:      slices.Clone(cfg.Tools.Disabled),
		maxOutput:     strconv.Itoa(cfg.Tools.MaxOutput),
		window:        strconv.Itoa(cfg.Context.Window),
		compactAt:     strconv.FormatFloat(*cfg.Context.CompactAt, 'f', -1, 64),
		strategy:      cfg.Context.Strategy,
		keepReasoning: *cfg.Context.KeepReasoning,
		systemPrompt:  cfg.SystemPrompt,
		descriptions:  map[string]*string{},
		memoryDir:     cfg.Memory.Dir,

		compactPrompt: cfg.Context.CompactPrompt,
		maxTokens:     strconv.Itoa(cfg.MaxTokens),
		search:        cfg.Web.Search,
		searchURL:     cfg.Web.SearchURL,
		searchKeyEnv:  cfg.Web.SearchKeyEnv,
		savedKey:      cfg.Web.SearchKey != "",
		images:        string(cfg.Render.Images),

		loopEnabled:       *cfg.Loop.Enabled,
		loopReplies:       strconv.Itoa(cfg.Loop.Replies),
		loopRepeats:       strconv.Itoa(cfg.Loop.Repeats),
		loopStreamRepeats: strconv.Itoa(cfg.Loop.StreamRepeats),
		loopWarnings:      strconv.Itoa(*cfg.Loop.Warnings),

		initialProvider: layer.Provider,
		initialModel:    model,
	}
	if draft.systemPrompt == "" {
		draft.systemPrompt = agent.DefaultSystemPrompt
	}
	if draft.compactPrompt == "" {
		draft.compactPrompt = agent.DefaultCompactPrompt
	}
	draft.savedDescriptions = cfg.Tools.Descriptions
	draft.describeTools(tools.BuiltinDescriptions())
	return draft
}

// describeTools sets the tools the form shows descriptions for, with their
// own descriptions, and the saved ones in their place.
func (d *settingsDraft) describeTools(defaults map[string]string) {
	d.defaultDescriptions = defaults
	d.descriptions = map[string]*string{}
	for name, description := range defaults {
		if custom, ok := d.savedDescriptions[name]; ok {
			description = custom
		}
		d.descriptions[name] = &description
	}
}

// applyTo writes the draft into the user layer. Provider and model are only
// written when they were changed, and every other value only when it differs
// from the built-in default.
func (d *settingsDraft) applyTo(layer config.Config, findings []detect.Finding) (config.Config, error) {
	maxOutput, err := parseCount(d.maxOutput)
	if err != nil {
		return layer, fmt.Errorf("max tool output: %w", err)
	}
	window, err := parseCount(d.window)
	if err != nil {
		return layer, fmt.Errorf("context window: %w", err)
	}
	compactAt, err := parseFraction(d.compactAt)
	if err != nil {
		return layer, fmt.Errorf("compact at: %w", err)
	}
	maxTokens, err := parseCount(d.maxTokens)
	if err != nil {
		return layer, fmt.Errorf("max output tokens: %w", err)
	}
	loop, err := d.loop()
	if err != nil {
		return layer, err
	}

	layer = d.applyProvider(layer, findings)
	defaults := config.Defaults()
	layer.Tools.Mode = unlessDefault(tools.Mode(d.mode), defaults.Tools.Mode)
	layer.Tools.AlwaysLoaded = unlessDefaultList(d.alwaysLoaded, defaults.Tools.AlwaysLoaded)
	layer.Tools.Confirm = unlessDefaultList(d.confirm, defaults.Tools.Confirm)
	layer.Tools.Disabled = unlessDefaultList(d.disabled, defaults.Tools.Disabled)
	layer.Tools.MaxOutput = unlessDefault(maxOutput, defaults.Tools.MaxOutput)
	layer.Tools.Descriptions = d.changedDescriptions()
	layer.Context.Window = window
	layer.Context.CompactAt = &compactAt
	if compactAt == *defaults.Context.CompactAt {
		layer.Context.CompactAt = nil
	}
	layer.Context.Strategy = unlessDefault(d.strategy, defaults.Context.Strategy)
	layer.Context.KeepReasoning = nil
	if d.keepReasoning != *defaults.Context.KeepReasoning {
		layer.Context.KeepReasoning = &d.keepReasoning
	}
	layer.SystemPrompt = unlessDefault(strings.TrimSpace(d.systemPrompt), agent.DefaultSystemPrompt)
	layer.Memory.Dir = unlessDefault(strings.TrimSpace(d.memoryDir), defaults.Memory.Dir)
	layer.Loop = loop
	layer.MaxTokens = maxTokens
	layer.Context.CompactPrompt = unlessDefault(strings.TrimSpace(d.compactPrompt), strings.TrimSpace(agent.DefaultCompactPrompt))
	layer.Web.Search = unlessDefault(d.search, defaults.Web.Search)
	layer.Web.SearchURL = strings.TrimSpace(d.searchURL)
	layer.Web.SearchKeyEnv = strings.TrimSpace(d.searchKeyEnv)
	if d.forgetKey {
		layer.Web.SearchKey = ""
	}
	if key := strings.TrimSpace(d.searchKey); key != "" {
		layer.Web.SearchKey = key
	}
	layer.Render.Images = unlessDefault(render.Mode(d.images), defaults.Render.Images)
	return layer, nil
}

// loop reads the loop guard fields, keeping only what differs from the
// built-in defaults.
func (d *settingsDraft) loop() (config.Loop, error) {
	defaults := config.Defaults().Loop
	replies, err := parseAtLeast(d.loopReplies, 1)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop replies: %w", err)
	}
	repeats, err := parseAtLeast(d.loopRepeats, 2)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop repeats: %w", err)
	}
	streamRepeats, err := parseAtLeast(d.loopStreamRepeats, 2)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop stream repeats: %w", err)
	}
	warnings, err := parseAtLeast(d.loopWarnings, 0)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop warnings: %w", err)
	}
	loop := config.Loop{
		Replies:       unlessDefault(replies, defaults.Replies),
		Repeats:       unlessDefault(repeats, defaults.Repeats),
		StreamRepeats: unlessDefault(streamRepeats, defaults.StreamRepeats),
	}
	if d.loopEnabled != *defaults.Enabled {
		loop.Enabled = &d.loopEnabled
	}
	if warnings != *defaults.Warnings {
		loop.Warnings = &warnings
	}
	return loop, nil
}

// applyProvider records a changed provider, adding a detected one to the
// providers, and a changed model where the user file already keeps it, or
// as the top-level model when the provider is picked automatically.
func (d *settingsDraft) applyProvider(layer config.Config, findings []detect.Finding) config.Config {
	model := strings.TrimSpace(d.model)
	providerChanged := d.provider != d.initialProvider
	modelChanged := model != d.initialModel
	if !providerChanged && !modelChanged {
		return layer
	}
	layer.Providers = maps.Clone(layer.Providers)
	if layer.Providers == nil {
		layer.Providers = map[string]config.ProviderConfig{}
	}
	if providerChanged {
		layer.Provider = d.provider
		_, known := config.Defaults().Providers[d.provider]
		_, saved := layer.Providers[d.provider]
		if found := findingFor(findings, d.provider); found != nil && found.Settings != nil && !known && !saved {
			layer.Providers[d.provider] = *found.Settings
		}
	}
	if modelChanged {
		if layer.Model != "" || d.provider == autoProvider {
			layer.Model = model
		} else {
			entry := layer.Providers[d.provider]
			entry.Model = model
			layer.Providers[d.provider] = entry
		}
	}
	return layer
}

// changedDescriptions keeps the edited descriptions that differ from the
// tool's own, and the saved ones of tools not loaded now.
func (d *settingsDraft) changedDescriptions() map[string]string {
	changed := map[string]string{}
	for name, description := range d.savedDescriptions {
		if _, shown := d.descriptions[name]; !shown {
			changed[name] = description
		}
	}
	for name, description := range d.descriptions {
		trimmed := strings.TrimSpace(*description)
		if trimmed != strings.TrimSpace(d.defaultDescriptions[name]) {
			changed[name] = trimmed
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return changed
}

func parseCount(text string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("use a whole number, 0 for the default")
	}
	return value, nil
}

func parseAtLeast(text string, least int) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || value < least {
		return 0, fmt.Errorf("use a whole number, %d or more", least)
	}
	return value, nil
}

func parseFraction(text string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || value < 0 || value >= 1 {
		return 0, fmt.Errorf("use a fraction from 0 (never) to below 1")
	}
	return value, nil
}

func unlessDefault[T comparable](value, builtin T) T {
	var zero T
	if value == builtin {
		return zero
	}
	return value
}

// unlessDefaultList returns an empty list, not nil, for "none ticked":
// "confirm = []" asks for nothing, while leaving the key out keeps the default.
func unlessDefaultList(value, builtin []string) []string {
	if slices.Equal(value, builtin) {
		return nil
	}
	if value == nil {
		return []string{}
	}
	return value
}

func findingFor(findings []detect.Finding, provider string) *detect.Finding {
	for i := range findings {
		if findings[i].Provider == provider && findings[i].Usable {
			return &findings[i]
		}
	}
	return nil
}

// followProvider takes the provider and model of layer, chosen in the
// provider form, as the ones the draft starts from.
func (d *settingsDraft) followProvider(layer config.Config) {
	fresh := newSettingsDraft(layer)
	d.provider, d.initialProvider = fresh.provider, fresh.initialProvider
	d.model, d.initialModel = fresh.model, fresh.initialModel
}

// snapshot returns what puts the draft back as it is now.
func (d *settingsDraft) snapshot() func() {
	saved := *d
	saved.alwaysLoaded = slices.Clone(d.alwaysLoaded)
	saved.confirm = slices.Clone(d.confirm)
	saved.disabled = slices.Clone(d.disabled)
	texts := map[string]string{}
	for name, text := range d.descriptions {
		texts[name] = *text
	}
	return func() {
		descriptions := d.descriptions
		*d = saved
		d.descriptions = descriptions
		for name, text := range texts {
			*descriptions[name] = text
		}
	}
}
