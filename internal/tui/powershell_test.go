package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PS-01 | Happy    | With PowerShell a shell call is headed powershell, by either name         | P0       |
// | PS-02 | Happy    | Its command is highlighted as PowerShell                                 | P2       |
// | PS-03 | Security | Approving a PowerShell command offers Yes and No, no rule to add          | P0       |
// | PS-04 | Happy    | The status line names the program a PowerShell command starts with       | P2       |
// | PS-05 | Happy    | With bash, the header stays bash and rules can be added                  | P1       |

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func approvalDialog(request agent.ApprovalRequest) (*dialog, dialogItem) {
	d := &dialog{groups: []*dialogGroup{{interaction: agent.Interaction{Approvals: []agent.ApprovalRequest{request}}}}}
	return d, dialogItem{group: 0, approval: 0, question: -1}
}

func TestPowerShellCalls(t *testing.T) {
	args := json.RawMessage(`{"command":"Get-ChildItem -Force"}`)
	ruled := agent.ApprovalRequest{Call: llm.ToolCall{Name: "bash", Args: args}, Ruled: true, Commands: []string{"Get-ChildItem -Force"}}

	t.Run("PS-01 With PowerShell a shell call is headed powershell, by either name", func(t *testing.T) {
		defer tools.UseShell(tools.PowerShellForTests(false))()
		p := newPrinter()

		canonical := ansi.Strip(p.ToolStart("bash", args, "Get-ChildItem -Force"))
		asModel := ansi.Strip(p.ToolStart("powershell", args, "Get-ChildItem -Force"))

		for _, header := range []string{canonical, asModel} {
			if !strings.Contains(header, "powershell") || strings.Contains(header, "bash") {
				t.Errorf("header = %q", header)
			}
		}
	})

	t.Run("PS-02 Its command is highlighted as PowerShell", func(t *testing.T) {
		defer tools.UseShell(tools.PowerShellForTests(false))()

		if language := commandLanguage("powershell", args); language != "powershell" {
			t.Errorf("language = %q", language)
		}
	})

	t.Run("PS-03 Approving a PowerShell command offers Yes and No, no rule to add", func(t *testing.T) {
		defer tools.UseShell(tools.PowerShellForTests(false))()
		d, item := approvalDialog(ruled)

		if buttons := d.buttonsFor(item); !slices.Equal(buttons, []string{"Yes", "No"}) {
			t.Errorf("buttons = %v", buttons)
		}
	})

	t.Run("PS-04 The status line names the program a PowerShell command starts with", func(t *testing.T) {
		defer tools.UseShell(tools.PowerShellForTests(false))()

		if shown := programs("Get-ChildItem -Force | Select-Object Name"); shown != "Get-ChildItem" {
			t.Errorf("programs = %q", shown)
		}
	})

	t.Run("PS-05 With bash, the header stays bash and rules can be added", func(t *testing.T) {
		p := newPrinter()
		d, item := approvalDialog(ruled)

		header := ansi.Strip(p.ToolStart("bash", args, "ls"))

		if tools.CurrentShell().PowerShell() || !strings.Contains(header, "bash") || !slices.Contains(d.buttonsFor(item), "Always… add a rule") {
			t.Errorf("header = %q buttons = %v", header, d.buttonsFor(item))
		}
	})
}
