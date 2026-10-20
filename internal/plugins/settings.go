package plugins

import (
	"slices"

	lua "github.com/yuin/gopher-lua"
)

// Setting is a value of a plugin the user sets in Settings › Plugins,
// declared with mo.setting.
type Setting struct {
	Name        string
	Type        string
	Default     any
	Description string
	Secret      bool
}

const (
	SettingString  = "string"
	SettingNumber  = "number"
	SettingBoolean = "boolean"

	// EnabledKey, false in a plugin's settings, keeps it from loading.
	EnabledKey = "enabled"
)

var settingTypes = []string{SettingString, SettingNumber, SettingBoolean}

// Enabled tells whether a plugin with these settings loads: unless they
// say enabled = false.
func Enabled(values map[string]any) bool {
	on, set := values[EnabledKey].(bool)
	return on || !set
}

// HasType tells whether value fits a setting of type kind.
func HasType(value any, kind string) bool {
	switch value.(type) {
	case string:
		return kind == SettingString
	case int64, float64:
		return kind == SettingNumber
	case bool:
		return kind == SettingBoolean
	}
	return false
}

func (p *Plugin) settingsTable() *lua.LTable {
	table := p.state.NewTable()
	table.RawSetString("get", p.state.NewFunction(p.getSetting))
	return table
}

// declareSetting is mo.setting{name, type, default, description, secret}.
func (p *Plugin) declareSetting(state *lua.LState) int {
	p.mustBeLoading(state, "mo.setting")
	spec := state.CheckTable(1)
	name := luaString(spec.RawGetString("name"))
	if !validName(name) || name == EnabledKey {
		state.RaiseError("mo.setting needs a name (a letter or _, then letters, digits, _ or -) other than enabled")
	}
	if p.setting(name) != nil {
		state.RaiseError("mo.setting: %s is declared twice", name)
	}
	kind := luaString(spec.RawGetString("type"))
	if kind == "" {
		kind = SettingString
	}
	if !slices.Contains(settingTypes, kind) {
		state.RaiseError("mo.setting %s: unknown type %q (use string, number or boolean)", name, kind)
	}
	setting := Setting{
		Name:        name,
		Type:        kind,
		Description: luaString(spec.RawGetString("description")),
		Secret:      lua.LVAsBool(spec.RawGetString("secret")),
	}
	if fallback := spec.RawGetString("default"); fallback != lua.LNil {
		value, err := toGo(fallback)
		if err != nil || !HasType(value, kind) {
			state.RaiseError("mo.setting %s: the default is not a %s", name, kind)
		}
		setting.Default = value
	}
	p.settings = append(p.settings, setting)
	return 0
}

// getSetting is mo.settings.get(name): the value the user set, or the
// default when there is none or it is not of the setting's type.
func (p *Plugin) getSetting(state *lua.LState) int {
	name := state.CheckString(1)
	setting := p.setting(name)
	if setting == nil {
		state.RaiseError("mo.settings.get: no setting %s; declare it with mo.setting", name)
	}
	value := setting.Default
	if set, ok := p.values[name]; ok && HasType(set, setting.Type) {
		value = set
	}
	state.Push(toLua(state, value))
	return 1
}

func (p *Plugin) setting(name string) *Setting {
	for i := range p.settings {
		if p.settings[i].Name == name {
			return &p.settings[i]
		}
	}
	return nil
}
