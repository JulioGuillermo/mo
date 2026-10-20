package tui

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/jg/mo/internal/plugins"
)

// clonePluginSettings copies the settings of every plugin, so editing the
// copy leaves the original as it was.
func clonePluginSettings(settings map[string]map[string]any) map[string]map[string]any {
	if settings == nil {
		return nil
	}
	clone := make(map[string]map[string]any, len(settings))
	for name, values := range settings {
		clone[name] = maps.Clone(values)
	}
	return clone
}

// pluginSettingsToSave are the plugins' settings without the plugins left
// with none.
func (d *settingsDraft) pluginSettingsToSave() map[string]map[string]any {
	saved := map[string]map[string]any{}
	for name, values := range d.plugins {
		if len(values) > 0 {
			saved[name] = maps.Clone(values)
		}
	}
	if len(saved) == 0 {
		return nil
	}
	return saved
}

func (d *settingsDraft) setPluginValue(plugin, name string, value any) {
	if d.plugins == nil {
		d.plugins = map[string]map[string]any{}
	}
	if d.plugins[plugin] == nil {
		d.plugins[plugin] = map[string]any{}
	}
	d.plugins[plugin][name] = value
}

func (d *settingsDraft) unsetPluginValue(plugin, name string) {
	delete(d.plugins[plugin], name)
}

// pluginOn edits whether a plugin loads; only off is written down.
type pluginOn struct {
	draft  *settingsDraft
	plugin string
}

func (p *pluginOn) Get() bool {
	return plugins.Enabled(p.draft.plugins[p.plugin])
}

func (p *pluginOn) Set(on bool) {
	if on {
		p.draft.unsetPluginValue(p.plugin, plugins.EnabledKey)
		return
	}
	p.draft.setPluginValue(p.plugin, plugins.EnabledKey, false)
}

// pluginValue edits one setting of a plugin as text. What equals the
// default, or is left empty, is not written down.
type pluginValue struct {
	draft   *settingsDraft
	plugin  string
	setting plugins.Setting
}

func (p *pluginValue) value() (any, bool) {
	value, set := p.draft.plugins[p.plugin][p.setting.Name]
	return value, set
}

func (p *pluginValue) Get() string {
	value, set := p.value()
	if !set {
		value = p.setting.Default
	}
	return settingText(value)
}

func (p *pluginValue) Set(text string) {
	value := any(text)
	if p.setting.Type == plugins.SettingNumber {
		value = parseSettingNumber(text)
	}
	if text == "" || value == p.setting.Default {
		p.draft.unsetPluginValue(p.plugin, p.setting.Name)
		return
	}
	p.draft.setPluginValue(p.plugin, p.setting.Name, value)
}

// pluginFlag edits a boolean setting of a plugin.
type pluginFlag struct{ pluginValue }

func (p *pluginFlag) Get() bool {
	value, set := p.value()
	if !set {
		value = p.setting.Default
	}
	on, _ := value.(bool)
	return on
}

func (p *pluginFlag) Set(on bool) {
	fallback, _ := p.setting.Default.(bool)
	if on == fallback {
		p.draft.unsetPluginValue(p.plugin, p.setting.Name)
		return
	}
	p.draft.setPluginValue(p.plugin, p.setting.Name, on)
}

// shown is the value of the setting for its row: hidden when secret, the
// default when unset.
func (p *pluginValue) shown() string {
	value, set := p.value()
	switch {
	case p.setting.Secret && set && value != "":
		return "••••••"
	case set:
		return textValue(settingText(value))
	case p.setting.Default == nil || p.setting.Secret:
		return "not set"
	}
	return textValue(settingText(p.setting.Default)) + " (default)"
}

func settingText(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		return yesNo(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

// parseSettingNumber is the number text holds, as an int64 when whole, or
// text itself when it holds none.
func parseSettingNumber(text string) any {
	text = strings.TrimSpace(text)
	if whole, err := strconv.ParseInt(text, 10, 64); err == nil {
		return whole
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number
	}
	return text
}

func validSettingNumber(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if _, isText := parseSettingNumber(text).(string); isText {
		return errors.New("write a number")
	}
	return nil
}
