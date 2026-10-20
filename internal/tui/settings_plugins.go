package tui

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/plugins"
)

const pluginsKey = "plugins"

var pluginsSection = settingsSection{
	key: pluginsKey, title: "Plugins", summary: pluginsSummary, note: pluginsNote, items: pluginItems,
}

func pluginsSummary(d *settingsDraft) string {
	off := 0
	for _, values := range d.plugins {
		if !plugins.Enabled(values) {
			off++
		}
	}
	if off == 0 {
		return "all on"
	}
	return fmt.Sprintf("%d off", off)
}

func pluginsNote(model, *settingsMenu) string {
	return "the .lua files in " + config.PluginDir() + "\nwhat you change here loads them again once saved"
}

// pluginItems are the plugins mo found: a row that turns each on or off,
// or, for one with settings, opens them.
func pluginItems(m model, menu *settingsMenu) []settingItem {
	var items []settingItem
	for _, status := range m.session.Plugins.Status() {
		if len(status.Settings) > 0 {
			menu.opens(itemKey(settingsSection{key: pluginsKey}, status.Name), pluginSection(status))
		}
		items = append(items, settingItem{
			title: status.Name,
			value: func(d *settingsDraft) string { return pluginState(d, status) },
			field: func(d *settingsDraft) huh.Field { return pluginSwitch(d, status.Name) },
		})
	}
	return items
}

// pluginSection is a plugin's own list: on or off, then its settings.
func pluginSection(status plugins.Status) settingsSection {
	return settingsSection{
		key: pluginsKey + ":" + status.Name, title: status.Name, parent: pluginsKey, from: status.Name,
		summary: func(d *settingsDraft) string { return pluginState(d, status) },
		note:    func(model, *settingsMenu) string { return pluginDetails(status) },
		items: func(model, *settingsMenu) []settingItem {
			items := []settingItem{{
				title: "On",
				value: func(d *settingsDraft) string { return onOff(plugins.Enabled(d.plugins[status.Name])) },
				field: func(d *settingsDraft) huh.Field { return pluginSwitch(d, status.Name) },
			}}
			for _, setting := range status.Settings {
				items = append(items, pluginSettingItem(status.Name, setting))
			}
			return items
		},
	}
}

func pluginSwitch(d *settingsDraft, name string) huh.Field {
	return huh.NewConfirm().Title(name).Description("off: the plugin does not run").
		Affirmative("On").Negative("Off").Accessor(&pluginOn{draft: d, plugin: name})
}

func pluginSettingItem(plugin string, setting plugins.Setting) settingItem {
	return settingItem{
		title: setting.Name,
		value: func(d *settingsDraft) string {
			value := pluginValue{draft: d, plugin: plugin, setting: setting}
			return value.shown()
		},
		field: func(d *settingsDraft) huh.Field {
			value := pluginValue{draft: d, plugin: plugin, setting: setting}
			if setting.Type == plugins.SettingBoolean {
				return huh.NewConfirm().Title(setting.Name).Description(setting.Description).
					Affirmative("Yes").Negative("No").Accessor(&pluginFlag{value})
			}
			input := huh.NewInput().Title(setting.Name).Description(settingDescription(setting)).Accessor(&value)
			if setting.Secret {
				input.EchoMode(huh.EchoModePassword)
			}
			if setting.Type == plugins.SettingNumber {
				input.Validate(validSettingNumber)
			}
			return input
		},
	}
}

func settingDescription(setting plugins.Setting) string {
	description := setting.Description
	if setting.Default != nil && !setting.Secret {
		description = strings.TrimSpace(description + "\nempty: " + settingText(setting.Default))
	}
	return description
}

// pluginState is what a plugin's row says: off, or on with what it offers
// or why it failed.
func pluginState(d *settingsDraft, status plugins.Status) string {
	switch {
	case !plugins.Enabled(d.plugins[status.Name]):
		return "off"
	case status.Disabled:
		return "on once saved"
	case status.Err != nil:
		return "✗ " + status.Err.Error()
	}
	offers := []string{"on"}
	for _, count := range []struct {
		n    int
		what string
	}{{len(status.Tools), "tool"}, {len(status.Commands), "command"}, {len(status.Hooks), "hook"}, {len(status.Settings), "setting"}} {
		switch {
		case count.n == 1:
			offers = append(offers, "1 "+count.what)
		case count.n > 1:
			offers = append(offers, fmt.Sprintf("%d %ss", count.n, count.what))
		}
	}
	return strings.Join(offers, " · ")
}

// pluginDetails lists what a plugin offers and the warnings it got.
func pluginDetails(status plugins.Status) string {
	var lines []string
	if status.Err != nil {
		lines = append(lines, "✗ "+status.Err.Error())
	}
	var offers []string
	offers = append(offers, status.Tools...)
	offers = append(offers, status.Commands...)
	offers = append(offers, status.Hooks...)
	if len(offers) > 0 {
		lines = append(lines, strings.Join(offers, ", "))
	}
	for _, warning := range status.Warnings {
		lines = append(lines, "! "+warning)
	}
	return strings.Join(lines, "\n")
}
