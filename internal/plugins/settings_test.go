package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PS-01 | Happy    | mo.settings.get gives the value the user set, else the declared default  | P0       |
// | PS-02 | Edge     | A value of another type than the setting's gives the default             | P1       |
// | PS-03 | Error    | A bad mo.setting fails the load: enabled, unknown type, wrong default, twice | P1    |
// | PS-04 | Error    | mo.settings.get of an undeclared setting is an error                     | P1       |
// | PS-05 | Happy    | enabled = false keeps a plugin from running; Status lists it off         | P0       |
// | PS-06 | Happy    | Status carries the settings a plugin declares                            | P1       |
// | PS-07 | Happy    | A set taking over tells the old one's followers of its changes; the old one closes | P0       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/tools"
)

const settingsPlugin = `
mo.setting{ name = "site", default = "example.org", description = "Where." }
mo.setting{ name = "limit", type = "number", default = 10 }
mo.setting{ name = "token", secret = true }
mo.setting{ name = "verbose", type = "boolean", default = false }
mo.tool{ name = "show", run = function()
  return mo.json_encode{ site = mo.settings.get("site"), limit = mo.settings.get("limit"),
    token = mo.settings.get("token") or "none", verbose = mo.settings.get("verbose") }
end }`

func loadWithSettings(t *testing.T, sources map[string]string, settings map[string]map[string]any) *Set {
	t.Helper()
	dir := t.TempDir()
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(dir, name+".lua"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	set := Load(dir, t.TempDir(), t.TempDir(), settings)
	t.Cleanup(set.Close)
	return set
}

func runFirstTool(t *testing.T, set *Set) string {
	t.Helper()
	output, err := set.Tools(nil)[0].Run(context.Background(), tools.Env{}, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestSettings(t *testing.T) {
	t.Run("PS-01 mo.settings.get gives the value the user set, else the declared default", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{"demo": settingsPlugin},
			map[string]map[string]any{"demo": {"limit": int64(3), "token": "s3cret", "verbose": true}})

		output := runFirstTool(t, set)

		want := `{"limit":3,"site":"example.org","token":"s3cret","verbose":true}`
		if output != want {
			t.Errorf("output = %s, want %s", output, want)
		}
	})

	t.Run("PS-02 a value of another type than the setting's gives the default", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{"demo": settingsPlugin},
			map[string]map[string]any{"demo": {"limit": "many", "site": int64(4)}})

		output := runFirstTool(t, set)

		if !strings.Contains(output, `"limit":10`) || !strings.Contains(output, `"site":"example.org"`) {
			t.Errorf("output = %s", output)
		}
	})

	t.Run("PS-03 a bad mo.setting fails the load: enabled, unknown type, wrong default, twice", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{
			"enabled": `mo.setting{ name = "enabled", type = "boolean" }`,
			"kind":    `mo.setting{ name = "colour", type = "color" }`,
			"wrong":   `mo.setting{ name = "limit", type = "number", default = "ten" }`,
			"twice":   `mo.setting{ name = "a" } mo.setting{ name = "a" }`,
		}, nil)

		for _, status := range set.Status() {
			if status.Err == nil {
				t.Errorf("plugin %s loaded", status.Name)
			}
		}
	})

	t.Run("PS-04 mo.settings.get of an undeclared setting is an error", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{"demo": `mo.settings.get("nope")`}, nil)

		status := set.Status()[0]

		if status.Err == nil || !strings.Contains(status.Err.Error(), "no setting nope") {
			t.Errorf("error = %v", status.Err)
		}
	})

	t.Run("PS-05 enabled = false keeps a plugin from running; Status lists it off", func(t *testing.T) {
		workDir := t.TempDir()
		dir := t.TempDir()
		source := `mo.write("ran", "yes") mo.tool{ name = "t", run = function() return "" end }`
		if err := os.WriteFile(filepath.Join(dir, "demo.lua"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}

		set := Load(dir, workDir, t.TempDir(), map[string]map[string]any{"demo": {"enabled": false}})
		t.Cleanup(set.Close)

		_, err := os.Stat(filepath.Join(workDir, "ran"))
		statuses := set.Status()
		if err == nil || len(set.Tools(nil)) != 0 || len(statuses) != 1 || !statuses[0].Disabled || statuses[0].Name != "demo" {
			t.Errorf("ran = %v statuses = %+v", err == nil, statuses)
		}
	})

	t.Run("PS-06 Status carries the settings a plugin declares", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{"demo": settingsPlugin}, nil)

		settings := set.Status()[0].Settings

		if len(settings) != 4 || settings[0] != (Setting{Name: "site", Type: SettingString, Default: "example.org", Description: "Where."}) ||
			settings[1].Default != int64(10) || !settings[2].Secret || settings[3].Type != SettingBoolean {
			t.Errorf("settings = %+v", settings)
		}
	})

	t.Run("PS-07 a set taking over tells the old one's followers of its changes; the old one closes", func(t *testing.T) {
		previous := loadWithSettings(t, map[string]string{"demo": `mo.sidebar.set{ id = "a", text = "old" }`}, nil)
		changes := make(chan struct{}, 4)
		previous.Follow(func() { changes <- struct{}{} }, nil)
		next := loadWithSettings(t, map[string]string{"demo": `mo.tool{ name = "t", run = function() return "" end }`}, nil)

		next.TakeOver(previous)
		next.surface.tell()
		select {
		case <-changes:
		case <-time.After(time.Second):
			t.Fatal("the new set does not tell the follower")
		}
		if !closedSoon(previous.plugins[0]) {
			t.Error("the previous set is still open")
		}
	})
}

// closedSoon tells whether the plugin's turn is taken for good within a
// second, as Close leaves it.
func closedSoon(plugin *Plugin) bool {
	deadline := time.After(time.Second)
	for {
		select {
		case token := <-plugin.turn:
			plugin.turn <- token
			time.Sleep(10 * time.Millisecond)
		case <-time.After(200 * time.Millisecond):
			return true
		case <-deadline:
			return false
		}
	}
}
