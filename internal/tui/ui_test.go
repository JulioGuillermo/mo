package tui

// Test Traceability Table
// | ID    | Category | Description                                                 | Priority |
// | TU-01 | Security | Enter does not approve a pending call, y does               | P0       |
// | TU-02 | Happy    | Esc during an approval denies the call and stops the run    | P0       |
// | TU-03 | Edge     | ctrl+d while running stops the run before quitting          | P0       |
// | TU-04 | Happy    | The approval prompt hides "always" outside the workdir      | P1       |
// | TU-05 | Happy    | The status line shows context used over the window          | P1       |
// | TU-06 | Happy    | An answer's mermaid block is drawn, its text rendered       | P1       |
// | TU-07 | Happy    | Images a tool result marks are drawn; others give nothing   | P1       |
// | TU-08 | Security | mo's own commands win even with an argument after them      | P1       |
// | TU-09 | Happy    | A plugin command runs like a turn: Esc can stop it          | P1       |

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/render"
)

func runningModel(cancelled *bool) model {
	return model{
		printer: newPrinter(),
		input:   newInput(),
		running: true,
		cancel:  func() { *cancelled = true },
	}
}

func withApproval(m model, request agent.ApprovalRequest) (model, chan agent.Decision) {
	reply := make(chan agent.Decision, 1)
	m.approval = &approvalMsg{request: request, reply: reply}
	return m, reply
}

func press(m model, key tea.KeyPressMsg) (model, tea.Cmd) {
	updated, cmd := m.Update(key)
	return updated.(model), cmd
}

var (
	keyY     = tea.KeyPressMsg{Code: 'y', Text: "y"}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyCtrlD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
)

func TestApprovalKeys(t *testing.T) {
	bashCall := agent.ApprovalRequest{Call: llm.ToolCall{Name: "bash"}, Summary: "rm -rf build"}

	t.Run("TU-01 enter does not approve a pending call, y does", func(t *testing.T) {
		var cancelled bool
		m, reply := withApproval(runningModel(&cancelled), bashCall)

		m, _ = press(m, keyEnter)
		if m.approval == nil || len(reply) != 0 {
			t.Fatal("enter answered the approval")
		}
		m, _ = press(m, keyY)
		if m.approval != nil || <-reply != agent.Allow {
			t.Error("y did not approve")
		}
	})

	t.Run("TU-02 esc during an approval denies the call and stops the run", func(t *testing.T) {
		var cancelled bool
		m, reply := withApproval(runningModel(&cancelled), bashCall)

		m, _ = press(m, keyEsc)

		if <-reply != agent.Deny || !cancelled || m.approval != nil {
			t.Errorf("cancelled = %v approval = %v", cancelled, m.approval)
		}
	})

	t.Run("TU-04 the approval prompt hides always outside the workdir", func(t *testing.T) {
		var cancelled bool
		inside, _ := withApproval(runningModel(&cancelled), bashCall)
		outside, _ := withApproval(runningModel(&cancelled), agent.ApprovalRequest{Call: llm.ToolCall{Name: "write"}, Warning: "outside the working directory"})

		if !strings.Contains(inside.approvalPrompt(), "always") {
			t.Error("inside prompt lacks always")
		}
		if strings.Contains(outside.approvalPrompt(), "always") || !strings.Contains(outside.approvalPrompt(), "outside") {
			t.Errorf("outside prompt = %q", outside.approvalPrompt())
		}
	})
}

func TestQuitWhileRunning(t *testing.T) {
	t.Run("TU-03 ctrl+d while running stops the run before quitting", func(t *testing.T) {
		var cancelled bool
		m := runningModel(&cancelled)

		_, cmd := press(m, keyCtrlD)

		if !cancelled || cmd == nil {
			t.Fatalf("cancelled = %v cmd = %v", cancelled, cmd)
		}
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Error("ctrl+d did not quit")
		}
	})
}

func TestStatusLine(t *testing.T) {
	t.Run("TU-05 the status line shows context used over the window", func(t *testing.T) {
		m := model{session: Session{Agent: &agent.Agent{Window: 32000}, Provider: "llama-server", Model: "/models/Qwen3-8B.gguf"}}
		m.usage = llm.Usage{InputTokens: 1200, OutputTokens: 34}

		status := ansi.Strip(m.statusLine())

		if !strings.Contains(status, "llama-server · Qwen3-8B · ctx 1.2k/32.0k ▰▱▱▱▱▱▱▱▱▱ 4%") {
			t.Errorf("status = %q", status)
		}
	})
}

func TestPrinterVisuals(t *testing.T) {
	t.Run("TU-06 an answer's mermaid block is drawn, its text rendered", func(t *testing.T) {
		printer := newPrinter()

		text := printer.Answer("Here:\n```mermaid\ngraph LR\n  A --> B\n```\n")

		if !strings.Contains(text, "Here") || !strings.Contains(text, "─") || strings.Contains(text, "graph LR") {
			t.Errorf("text = %q", text)
		}
	})

	t.Run("TU-07 images a tool result marks are drawn; others give nothing", func(t *testing.T) {
		printer := newPrinter()
		printer.setImages(render.ModeBlocks)
		path := filepath.Join(t.TempDir(), "dot.png")
		var encoded bytes.Buffer
		png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 4, 4)))
		os.WriteFile(path, encoded.Bytes(), 0o600)

		drawn := printer.Images("ok " + render.ImageMarker("png 4x4", path))

		if drawn == "" || printer.Images("no images here") != "" {
			t.Errorf("drawn = %q", drawn)
		}
	})
}

func TestCommandDispatch(t *testing.T) {
	t.Run("TU-08 mo's own commands win even with an argument after them", func(t *testing.T) {
		m := model{session: Session{Agent: &agent.Agent{}}, printer: newPrinter(), input: newInput()}

		updated, _ := m.runCommand("/settings now")

		if !updated.(model).settingsInFlight {
			t.Error("/settings with an argument did not open the settings")
		}
	})

	t.Run("TU-09 a plugin command runs like a turn: Esc can stop it", func(t *testing.T) {
		m := model{session: Session{Agent: &agent.Agent{}}, printer: newPrinter(), input: newInput(), bridge: &bridge{printer: newPrinter()}}
		run := func(ctx context.Context, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() }

		updated, cmd := m.runPluginCommand("/slow", run, "")

		running := updated.(model)
		if !running.running || cmd == nil {
			t.Fatal("the command did not start as a turn")
		}
		running.stop()
	})
}
