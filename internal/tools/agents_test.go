package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | AT-01 | Happy    | The agent tool's help explains every action, and its description only points to it | P1 |
// | AT-02 | Error    | tell_parent needs a message, and a parent                                | P2       |
// | AT-03 | Happy    | Kinds are left out of the description, which points to action types          | P0       |
// | AT-04 | Happy    | Listed, the description names the kinds in one line and type takes only them  | P0       |
// | AT-05 | Happy    | types lists the kinds one a line; with type, one in detail with its model and limits | P0 |
// | AT-06 | Error    | types names an unknown kind as such; with no kinds it says to leave type out  | P1       |
// | AT-07 | Happy    | Without kinds nothing tells of types: not the description, the actions, the schema nor the help | P0 |

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentTools(t *testing.T) {
	t.Run("AT-01 the agent tool's help explains every action, and its description only points to it", func(t *testing.T) {
		tool := agentTool(nil, false)
		var schema struct {
			Properties struct {
				Action struct {
					Enum []string `json:"enum"`
				} `json:"action"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(tool.Schema, &schema)

		text, err := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"help"}`), nil)

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

	kinds := []AgentKind{
		{Name: "Explore", Description: "read-only recon", Model: "lab/small", Effort: "low", Tools: []string{"read", "grep"}, Permissions: "ask", MaxSteps: 40},
		{Name: "fixer", CanStart: true},
	}

	t.Run("AT-03 kinds are left out of the description, which points to action types", func(t *testing.T) {
		tool := agentTool(kinds, false)

		if strings.Contains(tool.Description, "Explore") || strings.Contains(tool.Description, "read-only recon") ||
			!strings.Contains(tool.Description, `{"action":"types"} lists them`) || strings.Contains(string(tool.Schema), `"enum":["Explore"`) {
			t.Errorf("description %q\n%s", tool.Description, tool.Schema)
		}
		if !strings.Contains(string(tool.Schema), `"type":{"type":"string"}`) {
			t.Errorf("no type in %s", tool.Schema)
		}
	})

	t.Run("AT-04 listed, the description names the kinds in one line and type takes only them", func(t *testing.T) {
		tool := agentTool(kinds, true)

		if !strings.Contains(tool.Description, "\ntype: Explore, fixer; ") || strings.Contains(tool.Description, "read-only recon") ||
			!strings.Contains(string(tool.Schema), `"enum":["Explore","fixer"]`) {
			t.Errorf("description %q\n%s", tool.Description, tool.Schema)
		}
	})

	t.Run("AT-05 types lists the kinds one a line; with type, one in detail with its model and limits", func(t *testing.T) {
		list, listErr := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"types"}`), kinds)
		detail, detailErr := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"types","type":"Explore"}`), kinds)
		other, _ := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"types","type":"fixer"}`), kinds)

		if listErr != nil || !strings.HasPrefix(list, "Explore: read-only recon\nfixer: no description\n") {
			t.Errorf("list %q err %v", list, listErr)
		}
		for _, want := range []string{"Explore: read-only recon", "model: lab/small · effort low", "tools: read, grep", "skills: yours", "starts subagents: no", "or ask when stricter", "steps: at most 40"} {
			if detailErr != nil || !strings.Contains(detail, want) {
				t.Errorf("no %q in %q err %v", want, detail, detailErr)
			}
		}
		if !strings.Contains(other, "model: the subagents'") || !strings.Contains(other, "tools: yours") || !strings.Contains(other, "starts subagents: yes") || strings.Contains(other, "steps") {
			t.Errorf("other %q", other)
		}
	})

	t.Run("AT-06 types names an unknown kind as such; with no kinds it says to leave type out", func(t *testing.T) {
		_, unknown := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"types","type":"tester"}`), kinds)
		_, none := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"types"}`), nil)

		if unknown == nil || !strings.Contains(unknown.Error(), `"tester"`) || none == nil || !strings.Contains(none.Error(), "leave type out") {
			t.Errorf("unknown %v none %v", unknown, none)
		}
	})

	t.Run("AT-07 without kinds nothing tells of types: not the description, the actions, the schema nor the help", func(t *testing.T) {
		plain := agentTool(nil, false)
		withKinds := agentTool(kinds, false)
		plainHelp, _ := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"help"}`), nil)
		kindsHelp, _ := runAgent(context.Background(), Env{}, json.RawMessage(`{"action":"help"}`), kinds)

		for name, text := range map[string]string{"description": plain.Description, "schema": string(plain.Schema), "help": plainHelp} {
			if strings.Contains(text, "type:") || strings.Contains(text, "types") || strings.Contains(text, `"type":{"type":"string"}`) {
				t.Errorf("%s tells of types: %s", name, text)
			}
		}
		if !strings.Contains(string(withKinds.Schema), `"types"`) || !strings.Contains(kindsHelp, "types: ") || !strings.Contains(withKinds.Description, "a default one") {
			t.Errorf("with kinds: %s\n%s", withKinds.Schema, kindsHelp)
		}
	})
}
