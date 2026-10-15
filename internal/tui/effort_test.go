package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | EP-01 | Happy    | The status line names the effort set; none set, nothing                  | P1       |
// | EP-02 | Happy    | Tab after /effort offers default first, then what the model takes, the current marked | P0 |
// | EP-03 | Happy    | /effort alone opens a menu; when the provider says nothing it says so    | P0       |
// | EP-04 | Error    | A level the model does not take is refused naming those it does          | P1       |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func effortModel(t *testing.T, levels []string, known bool) model {
	t.Helper()
	m := conversationModel()
	m.width, m.height = 120, 30
	m.session.Model = "qwen3"
	m.session.Provider = "llama-server"
	m.efforts = effortInfo{key: effortKey(m.session), levels: levels, known: known, loaded: true}
	return m
}

func TestEffort(t *testing.T) {
	t.Run("EP-01 the status line names the effort set; none set, nothing", func(t *testing.T) {
		m := effortModel(t, []string{"low", "high"}, true)
		plain := ansi.Strip(m.statusLine())
		m.session.Agent.Effort = "high"
		set := ansi.Strip(m.statusLine())

		if strings.Contains(plain, " · high") || !strings.Contains(set, "qwen3 · high") {
			t.Errorf("plain = %q set = %q", plain, set)
		}
	})

	t.Run("EP-02 tab after /effort offers default first, then what the model takes, the current marked", func(t *testing.T) {
		m := effortModel(t, []string{"low", "high"}, true)
		m.session.Agent.Effort = "low"
		m.input.SetValue("/effort ")
		var names, notes []string
		for _, each := range m.suggestions() {
			names = append(names, each.name)
			notes = append(notes, each.description)
		}

		if strings.Join(names, ",") != "effort default,effort low,effort high" || notes[0] != "the model's own" || notes[1] != "now" {
			t.Errorf("names = %v notes = %v", names, notes)
		}
		m.input.SetValue("/effort h")
		if got := m.suggestions(); len(got) != 1 || got[0].name != "effort high" {
			t.Errorf("filtered = %v", got)
		}
	})

	t.Run("EP-03 /effort alone opens a menu; when the provider says nothing it says so", func(t *testing.T) {
		known, _ := effortModel(t, []string{"low", "high"}, true).runCommand("/effort")
		unknown, _ := effortModel(t, []string{"none", "low", "medium", "high", "xhigh", "max"}, false).runCommand("/effort")

		if menu := menuView(known.(model)); !strings.Contains(menu, "default: the model's own  ● now") || !strings.Contains(menu, "high") || strings.Contains(menu, "does not say") {
			t.Errorf("known:\n%s", menu)
		}
		if menu := menuView(unknown.(model)); !strings.Contains(menu, "does not say") || !strings.Contains(menu, "xhigh") {
			t.Errorf("unknown:\n%s", menu)
		}
	})

	t.Run("EP-04 a level the model does not take is refused naming those it does", func(t *testing.T) {
		refused, _ := effortModel(t, []string{"none"}, true).runCommand("/effort high")

		if shown := shown(refused.(model)); !strings.Contains(shown, "qwen3 takes default, none") {
			t.Errorf("shown:\n%s", shown)
		}
	})
}
