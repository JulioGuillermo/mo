package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | VS-01 | Happy    | Where images are drawn the model hears of images, svg and the mermaid it can draw | P0     |
// | VS-02 | Edge     | Without images only mermaid is told; with mermaid-cli and pictures, any diagram | P0       |
// | VS-03 | Happy    | The note goes after the system prompt of every request, and off in the settings sends none | P0 |

import (
	"context"
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/render"
)

func TestVisualsNote(t *testing.T) {
	t.Run("VS-01 where images are drawn the model hears of images, svg and the mermaid it can draw", func(t *testing.T) {
		note := visualsNote(render.ModeBlocks, false)

		want := "Your answers are drawn in a terminal that can render:\n" +
			"- local images, as a line of its own: ![name](path) (PNG, JPEG, GIF, WebP, SVG)\n" +
			"- svg code blocks\n" +
			"- mermaid code blocks: flowchart, sequence, ER and class diagrams\n" +
			"Use them when a picture explains better than text."
		if note != want {
			t.Errorf("note:\n%s", note)
		}
	})

	t.Run("VS-02 without images only mermaid is told; with mermaid-cli and pictures, any diagram", func(t *testing.T) {
		off := visualsNote(render.ModeOff, true)
		kitty := visualsNote(render.ModeKitty, true)
		blocks := visualsNote(render.ModeBlocks, true)

		if strings.Contains(off, "images") || strings.Contains(off, "svg") || !strings.Contains(off, "flowchart, sequence") {
			t.Errorf("off:\n%s", off)
		}
		if !strings.Contains(kitty, "mermaid code blocks: any diagram") {
			t.Errorf("kitty:\n%s", kitty)
		}
		if !strings.Contains(blocks, "flowchart, sequence") {
			t.Errorf("blocks, whose pictures mermaid-cli cannot use:\n%s", blocks)
		}
	})

	t.Run("VS-03 the note goes after the system prompt of every request, and off in the settings sends none", func(t *testing.T) {
		provider := &recording{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "a"}, {Role: llm.RoleAssistant, Text: "b"}}}
		m, _ := btwModel(t, provider)
		m.session.Agent.System = "be brief"
		m.printer.setImages(render.ModeBlocks)

		m.tellVisuals()
		if err := m.session.Agent.Run(context.Background(), "one", agent.Hooks{}); err != nil {
			t.Fatal(err)
		}
		off := false
		m.session.Config.Render.TellModel = &off
		m.tellVisuals()
		if err := m.session.Agent.Run(context.Background(), "two", agent.Hooks{}); err != nil {
			t.Fatal(err)
		}

		if told := provider.requests[0].System; told != "be brief\n\n"+visualsNote(render.ModeBlocks, false) {
			t.Errorf("told:\n%s", told)
		}
		if quiet := provider.requests[1].System; quiet != "be brief" {
			t.Errorf("with the setting off:\n%s", quiet)
		}
	})
}
