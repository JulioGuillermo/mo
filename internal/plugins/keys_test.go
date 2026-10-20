package plugins

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | KY-01 | Happy    | mo.on("key") hears keys only while its plugin is the one in focus; true takes the key | P0 |
// | KY-02 | Happy    | Patterns: one key, a list, mod+* for every key with those modifiers, none for all | P0 |
// | KY-03 | Security | mo.global.on hears keys anywhere only once global_events is on; asked or heard, GlobalEvents and Status say so | P0 |
// | KY-04 | Security | Esc and ctrl+c reach no plugin                                              | P0       |
// | KY-05 | Edge     | A listener slower than keyWait lets the key go on                           | P1       |
// | KY-06 | Error    | A bad pattern, an unknown global event, or global_events as a setting fails the load | P1 |
// | KY-07 | Happy    | The first listener that takes a key stops the rest; the plugin in focus goes first | P1 |

import (
	"strings"
	"testing"
	"time"
)

const keyLogger = `
heard = {}
LISTEN
mo.command{ name = "heard", run = function() return table.concat(heard, ",") end }`

func heardKeys(t *testing.T, set *Set) string {
	t.Helper()
	output, err := runCommand(t, set, "heard")
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func keyPlugin(t *testing.T, listen string, settings map[string]map[string]any) *Set {
	t.Helper()
	return loadWithSettings(t, map[string]string{"demo": strings.Replace(keyLogger, "LISTEN", listen, 1)}, settings)
}

func TestKeys(t *testing.T) {
	t.Run("KY-01 mo.on(\"key\") hears keys only while its plugin is the one in focus; true takes the key", func(t *testing.T) {
		set := keyPlugin(t, `mo.on("key", function(e) heard[#heard + 1] = e.key .. e.text return e.key == "j" end)`, nil)

		elsewhere := set.Key("", "j", "j")
		other := set.Key("other", "j", "j")
		taken := set.Key("demo", "j", "j")
		passed := set.Key("demo", "x", "x")

		if elsewhere || other || !taken || passed || heardKeys(t, set) != "jj,xx" {
			t.Errorf("elsewhere = %v other = %v taken = %v passed = %v heard = %q", elsewhere, other, taken, passed, heardKeys(t, set))
		}
	})

	t.Run("KY-02 patterns: one key, a list, mod+* for every key with those modifiers, none for all", func(t *testing.T) {
		cases := map[string]string{
			`"ctrl+x"`:              "ctrl+x",
			`{ "ctrl+x", "alt+y" }`: "ctrl+x,alt+y",
			`"ctrl+*"`:              "ctrl+x,ctrl+shift+z",
			`"ctrl+shift+*"`:        "ctrl+shift+z",
		}
		for pattern, want := range cases {
			set := keyPlugin(t, `mo.on("key", `+pattern+`, function(e) heard[#heard + 1] = e.key end)`, nil)
			for _, key := range []string{"ctrl+x", "alt+y", "ctrl+shift+z", "j", "alt+x"} {
				set.Key("demo", key, "")
			}
			if got := heardKeys(t, set); got != want {
				t.Errorf("%s heard %q, want %q", pattern, got, want)
			}
		}
		all := keyPlugin(t, `mo.on("key", function(e) heard[#heard + 1] = e.key end)`, nil)
		all.Key("demo", "j", "j")
		all.Key("demo", "f5", "")
		if got := heardKeys(t, all); got != "j,f5" {
			t.Errorf("no pattern heard %q", got)
		}
	})

	t.Run("KY-03 mo.global.on hears keys anywhere only once global_events is on; asked or heard, GlobalEvents and Status say so", func(t *testing.T) {
		listen := `mo.global.on("key", "ctrl+*", function(e) heard[#heard + 1] = e.key return true end)`
		off := keyPlugin(t, listen, nil)
		on := keyPlugin(t, listen, map[string]map[string]any{"demo": {GlobalEventsKey: true}})
		none := keyPlugin(t, ``, nil)

		offTook, onTook := off.Key("", "ctrl+g", ""), on.Key("", "ctrl+g", "")

		offAsked, offHeard := off.GlobalEvents()
		onAsked, onHeard := on.GlobalEvents()
		noneAsked, noneHeard := none.GlobalEvents()
		if offTook || heardKeys(t, off) != "" || !onTook || heardKeys(t, on) != "ctrl+g" {
			t.Errorf("off took %v heard %q; on took %v heard %q", offTook, heardKeys(t, off), onTook, heardKeys(t, on))
		}
		if !offAsked || offHeard || onAsked || !onHeard || noneAsked || noneHeard {
			t.Errorf("off %v/%v on %v/%v none %v/%v", offAsked, offHeard, onAsked, onHeard, noneAsked, noneHeard)
		}
		offStatus, onStatus := off.Status()[0], on.Status()[0]
		if strings.Join(offStatus.GlobalKeys, "") != "ctrl+*" || offStatus.GlobalHeard || !onStatus.GlobalHeard || len(none.Status()[0].GlobalKeys) != 0 {
			t.Errorf("off = %+v on = %+v", offStatus, onStatus)
		}
	})

	t.Run("KY-04 esc and ctrl+c reach no plugin", func(t *testing.T) {
		set := keyPlugin(t, `
mo.on("key", function(e) heard[#heard + 1] = e.key return true end)
mo.global.on("key", function(e) heard[#heard + 1] = e.key return true end)`, map[string]map[string]any{"demo": {GlobalEventsKey: true}})

		esc, stop := set.Key("demo", "esc", ""), set.Key("demo", "ctrl+c", "")

		if esc || stop || heardKeys(t, set) != "" {
			t.Errorf("esc = %v ctrl+c = %v heard %q", esc, stop, heardKeys(t, set))
		}
	})

	t.Run("KY-05 a listener slower than keyWait lets the key go on", func(t *testing.T) {
		set := keyPlugin(t, `mo.on("key", function() mo.sleep(0.3) return true end)`, nil)

		started := time.Now()
		took := set.Key("demo", "j", "j")

		waited := time.Since(started)
		heardKeys(t, set)
		time.Sleep(50 * time.Millisecond)

		if took || waited > 4*keyWait {
			t.Errorf("took = %v after %v", took, waited)
		}
	})

	t.Run("KY-06 a bad pattern, an unknown global event, or global_events as a setting fails the load", func(t *testing.T) {
		set := loadSource(t, map[string]string{
			"star":    `mo.on("key", "*", function() end)`,
			"middle":  `mo.on("key", "ctrl+*+x", function() end)`,
			"event":   `mo.global.on("prompt", function() end)`,
			"setting": `mo.setting{ name = "global_events", type = "boolean" }`,
		}).set

		for _, status := range set.Status() {
			if status.Err == nil {
				t.Errorf("plugin %s loaded", status.Name)
			}
		}
	})

	t.Run("KY-07 the first listener that takes a key stops the rest; the plugin in focus goes first", func(t *testing.T) {
		set := loadWithSettings(t, map[string]string{
			"a_global": `mo.global.on("key", function(e) mo.sidebar.set{ id = "g", text = "global " .. e.key } return true end)`,
			"b_focus":  `mo.on("key", function(e) mo.sidebar.set{ id = "f", text = "focus " .. e.key } return e.key == "j" end)`,
		}, map[string]map[string]any{"a_global": {GlobalEventsKey: true}})

		set.Key("b_focus", "j", "j")
		afterJ := len(set.SidebarItems())
		set.Key("b_focus", "k", "k")

		items := set.SidebarItems()
		if afterJ != 1 || len(items) != 2 || items[0].Text != "focus k" || items[1].Text != "global k" {
			t.Errorf("after j %d items, then %+v", afterJ, items)
		}
	})
}
