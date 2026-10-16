package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SU-01 | Happy    | What a subagent asks says who asks; the main agent's does not             | P0       |
// | SU-02 | Happy    | The Subagents settings are saved as they differ from the defaults, and wrong ones refused | P1 |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
)

func TestSubagentsUI(t *testing.T) {
	t.Run("SU-01 what a subagent asks says who asks; the main agent's does not", func(t *testing.T) {
		fromSubagent, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{bash("make test")}, From: "scout (agent 2, level 1)"})
		fromMain, _ := asking(agent.Interaction{Approvals: []agent.ApprovalRequest{bash("make test")}})

		if view := ansi.Strip(fromSubagent.dialogView()); !strings.Contains(view, "asked by subagent scout (agent 2, level 1)") {
			t.Errorf("subagent:\n%s", view)
		}
		if view := ansi.Strip(fromMain.dialogView()); strings.Contains(view, "asked by") {
			t.Errorf("main:\n%s", view)
		}
	})

	t.Run("SU-02 the Subagents settings are saved as they differ from the defaults, and wrong ones refused", func(t *testing.T) {
		draft := defaultDraft()
		draft.agentsEnabled, draft.agentsDepth, draft.agentsModel = true, "3", "llama-server/qwen3"
		layer, err := draft.applyTo(config.Config{}, nil)
		untouched, _ := defaultDraft().applyTo(config.Config{}, nil)
		draft.agentsModel = "qwen3"
		_, wrong := draft.applyTo(config.Config{}, nil)

		if err != nil || layer.Agents.Enabled == nil || !*layer.Agents.Enabled || layer.Agents.Depth != 3 || layer.Agents.MaxParallel != 0 || layer.Agents.Model != "llama-server/qwen3" {
			t.Errorf("err %v agents %+v", err, layer.Agents)
		}
		if untouched.Agents != (config.Agents{}) || wrong == nil {
			t.Errorf("untouched %+v wrong %v", untouched.Agents, wrong)
		}
	})
}
