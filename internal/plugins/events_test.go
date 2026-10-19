package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PE-01 | Happy    | A tool_result hook sees the call and its output, and what it returns is what the model reads | P0 |
// | PE-02 | Edge     | A tool_result hook that returns nothing or fails leaves the output       | P0       |
// | PE-03 | Happy    | A turn_end hook gets the prompt, the answer, the error and whether it was stopped | P0 |
// | PE-04 | Error    | mo.on names the events it takes when given another                       | P2       |

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestHookEvents(t *testing.T) {
	ctx := context.Background()
	call := llm.ToolCall{Name: "bash", Args: []byte(`{"command":"cat .env"}`)}

	t.Run("PE-01 a tool_result hook sees the call and its output, and what it returns is what the model reads", func(t *testing.T) {
		set := loadSource(t, map[string]string{"redact": `
mo.on("tool_result", function(event)
  if event.name ~= "bash" or event.is_error then return nil end
  return event.args.command .. ": " .. event.output:gsub("TOKEN=%S+", "TOKEN=***")
end)`}).set

		output := set.AfterToolCall(ctx, call, "TOKEN=abc123 USER=ana", false)

		if output != "cat .env: TOKEN=*** USER=ana" {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("PE-02 a tool_result hook that returns nothing or fails leaves the output", func(t *testing.T) {
		f := loadSource(t, map[string]string{
			"a_quiet":  `mo.on("tool_result", function() end)`,
			"b_broken": `mo.on("tool_result", function() error("oops") end)`,
		})

		output := f.set.AfterToolCall(ctx, call, "as it was", true)

		log, _ := os.ReadFile(filepath.Join(f.logDir, "plugin-b_broken.log"))
		if output != "as it was" || !strings.Contains(string(log), "oops") {
			t.Errorf("output = %q log = %q", output, log)
		}
	})

	t.Run("PE-03 a turn_end hook gets the prompt, the answer, the error and whether it was stopped", func(t *testing.T) {
		f := loadSource(t, map[string]string{"notify": `
mo.on("turn_end", function(event)
  local line = event.prompt .. "|" .. event.answer .. "|" .. tostring(event.error) .. "|" .. tostring(event.stopped) .. "\n"
  local file = io.open(mo.workdir .. "/ends.txt", "a")
  file:write(line)
  file:close()
end)`})

		f.set.EndTurn("fix it", "done", nil)
		f.set.EndTurn("again", "", context.Canceled)
		f.set.EndTurn("third", "", errors.New("429 Too Many Requests"))

		ends, _ := os.ReadFile(filepath.Join(f.workDir, "ends.txt"))
		want := "fix it|done|nil|false\nagain||context canceled|true\nthird||429 Too Many Requests|false\n"
		if string(ends) != want {
			t.Errorf("ends = %q", ends)
		}
	})

	t.Run("PE-04 mo.on names the events it takes when given another", func(t *testing.T) {
		set := loadSource(t, map[string]string{"typo": `mo.on("turn_ended", function() end)`}).set

		status := set.Status()[0]

		if status.Err == nil || !strings.Contains(status.Err.Error(), "prompt, tool_call, tool_result, turn_end") {
			t.Errorf("err = %v", status.Err)
		}
	})
}
