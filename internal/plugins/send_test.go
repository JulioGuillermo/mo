package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SN-01 | Happy    | mo.send hands the title, the message and the plugin's name over; wake is on unless false | P0 |
// | SN-02 | Error    | mo.send without a title or a message, or with one over 64 KB, fails      | P1       |
// | SN-03 | Edge     | With no conversation to take it, mo.send gives false and logs it         | P2       |

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/tools"
)

func TestSend(t *testing.T) {
	ctx := context.Background()

	t.Run("SN-01 mo.send hands the title, the message and the plugin's name over; wake is on unless false", func(t *testing.T) {
		set := loadSource(t, map[string]string{"jira": `
mo.tool{ name = "go", run = function()
  local first = mo.send{ title = "BSSP-1 moved", message = "It is in review." }
  mo.send{ title = "quiet", message = "for later", wake = false }
  return tostring(first)
end }`}).set
		got := make(chan SentMessage, 2)
		set.OnSend(func(message SentMessage) { got <- message })

		output, err := set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		first, second := <-got, <-got
		if first.Title == "quiet" {
			first, second = second, first
		}
		if err != nil || output != "true" {
			t.Fatalf("output = %q err = %v", output, err)
		}
		if first != (SentMessage{Plugin: "jira", Title: "BSSP-1 moved", Text: "It is in review.", Wake: true}) || second.Wake || second.Text != "for later" {
			t.Errorf("first = %+v second = %+v", first, second)
		}
	})

	t.Run("SN-02 mo.send without a title or a message, or with one over 64 KB, fails", func(t *testing.T) {
		set := loadSource(t, map[string]string{"bad": `
mo.tool{ name = "no_title", run = function() mo.send{ message = "x" } end }
mo.tool{ name = "huge", run = function() mo.send{ title = "t", message = string.rep("x", 70000) } end }`}).set
		set.OnSend(func(SentMessage) {})
		adapted := set.Tools(nil)

		_, noTitle := adapted[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))
		_, huge := adapted[1].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		if noTitle == nil || !strings.Contains(noTitle.Error(), "needs a title and a message") || huge == nil || !strings.Contains(huge.Error(), "64 KB") {
			t.Errorf("noTitle = %v huge = %v", noTitle, huge)
		}
	})

	t.Run("SN-03 with no conversation to take it, mo.send gives false and logs it", func(t *testing.T) {
		f := loadSource(t, map[string]string{"lonely": `
mo.tool{ name = "go", run = function() return tostring(mo.send{ title = "hi", message = "anyone?" }) end }`})

		output, _ := f.set.Tools(nil)[0].Run(ctx, tools.Env{}, json.RawMessage(`{}`))

		log, _ := os.ReadFile(filepath.Join(f.logDir, "plugin-lonely.log"))
		if output != "false" || !strings.Contains(string(log), "no conversation") {
			t.Errorf("output = %q log = %q", output, log)
		}
	})
}
