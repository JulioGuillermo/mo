package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AT-01 | Happy    | The agent tool's help explains every action, and its description only points to it | P1 |
// | AT-02 | Error    | tell_parent needs a message, and a parent                                | P2       |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentTools(t *testing.T) {
	t.Run("AT-01 the agent tool's help explains every action, and its description only points to it", func(t *testing.T) {
		tool := agentTool()
		var schema struct {
			Properties struct {
				Action struct {
					Enum []string `json:"enum"`
				} `json:"action"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(tool.Schema, &schema)

		text, err := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"help"}`))

		for _, action := range schema.Properties.Action.Enum {
			if action != "help" && !strings.Contains(text, action+": ") {
				t.Errorf("help says nothing of %s:\n%s", action, text)
			}
		}
		if err != nil || !strings.Contains(tool.Description, `{"action":"help"}`) || strings.Contains(tool.Description, "view") {
			t.Errorf("err %v description %q", err, tool.Description)
		}
	})

	t.Run("AT-02 tell_parent needs a message, and a parent", func(t *testing.T) {
		tool := tellParentTool()

		_, empty := tool.Run(context.Background(), Env{Parent: deafParent{}}, json.RawMessage(`{"message":" "}`))
		_, orphan := tool.Run(context.Background(), Env{}, json.RawMessage(`{"message":"hi"}`))
		told, err := tool.Run(context.Background(), Env{Parent: deafParent{}}, json.RawMessage(`{"message":"hi"}`))

		if empty == nil || orphan == nil || err != nil || told != "told" {
			t.Errorf("empty %v orphan %v told %q %v", empty, orphan, told, err)
		}
	})
}
