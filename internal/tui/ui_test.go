package tui

// Test Traceability Table
// | ID    | Category | Description                                                 | Priority |
// | TU-01 | Security | Enter alone never approves: it presses No, selected first; y approves | P0  |
// | TU-02 | Happy    | Esc during an approval denies the call and stops the run    | P0       |
// | TU-03 | Edge     | ctrl+d while running stops the run before quitting          | P0       |
// | TU-04 | Happy    | The approval dialog hides "always" outside the workdir      | P1       |
// | TU-05 | Happy    | The status line shows context used over the window          | P1       |
// | TU-06 | Happy    | An answer's mermaid block is drawn, its text rendered       | P1       |
// | TU-07 | Happy    | Images a tool result marks are drawn; others give nothing   | P1       |
// | TU-08 | Security | mo's own commands win even with an argument after them      | P1       |
// | TU-09 | Happy    | A plugin command runs like a turn: Esc can stop it          | P1       |
// | TU-10 | Happy    | Images line up with their text and keep inside the width    | P1       |
// | TU-11 | Happy    | After esc the note offers /continue, which starts a turn; not while one runs | P1 |
// | TU-12 | Happy    | Written while the model works, a message is queued; esc gives it back | P0     |
// | TU-13 | Happy    | Safe commands work while the model works; settings saved wait for the turn's end | P1 |

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
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
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

func withApproval(m model, request agent.ApprovalRequest) (model, chan agent.Reply) {
	reply := make(chan agent.Reply, 1)
	m.dialog = newDialog(interactionMsg{interaction: agent.Interaction{Approvals: []agent.ApprovalRequest{request}}, reply: reply})
	return m, reply
}

func decided(reply chan agent.Reply) agent.Decision {
	return (<-reply).Decisions[0]
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

	t.Run("TU-01 enter alone never approves: it presses No, selected first; y approves", func(t *testing.T) {
		var cancelled bool
		entered, enterReply := withApproval(runningModel(&cancelled), bashCall)
		typed, yReply := withApproval(runningModel(&cancelled), bashCall)

		entered, _ = press(entered, keyEnter)
		typed, _ = press(typed, keyY)

		if entered.dialog != nil || decided(enterReply) != agent.Deny {
			t.Error("enter did not press No")
		}
		if typed.dialog != nil || decided(yReply) != agent.Allow {
			t.Error("y did not approve")
		}
	})

	t.Run("TU-02 esc during an approval denies the call and stops the run", func(t *testing.T) {
		var cancelled bool
		m, reply := withApproval(runningModel(&cancelled), bashCall)

		m, _ = press(m, keyEsc)

		if decided(reply) != agent.Deny || !cancelled || m.dialog != nil {
			t.Errorf("cancelled = %v dialog = %v", cancelled, m.dialog)
		}
	})

	t.Run("TU-04 the approval prompt hides always outside the workdir", func(t *testing.T) {
		var cancelled bool
		inside, _ := withApproval(runningModel(&cancelled), bashCall)
		outside, _ := withApproval(runningModel(&cancelled), agent.ApprovalRequest{Call: llm.ToolCall{Name: "write"}, Warning: "outside the working directory"})

		if !strings.Contains(ansi.Strip(inside.dialogView()), "Always") {
			t.Error("inside dialog lacks always")
		}
		if view := ansi.Strip(outside.dialogView()); strings.Contains(view, "Always") || !strings.Contains(view, "outside") {
			t.Errorf("outside dialog = %q", view)
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

		if !strings.Contains(status, "llama-server › Qwen3-8B \ue0b0 ▰▱▱▱▱▱▱▱▱▱ 4% 1.2k/32.0k \ue0b0") {
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

func TestImageIndent(t *testing.T) {
	t.Run("TU-10 images line up with their text and keep inside the width", func(t *testing.T) {
		printer := newPrinter()
		printer.setWidth(40)
		printer.setImages(render.ModeBlocks)
		path := filepath.Join(t.TempDir(), "wide.png")
		var encoded bytes.Buffer
		_ = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 400, 40)))
		_ = os.WriteFile(path, encoded.Bytes(), 0o644)

		fromTool := printer.Images("shot " + render.ImageMarker("png 400x40", path))
		inAnswer := printer.File(path)

		for name, drawn := range map[string]string{"tool": fromTool, "answer": inAnswer} {
			indent := map[string]string{"tool": "    ", "answer": "  "}[name]
			for _, line := range strings.Split(drawn, "\n") {
				if !strings.HasPrefix(line, indent+"\x1b[") || ansi.StringWidth(line) > 40 {
					t.Errorf("%s line starts %q, %d wide", name, line[:min(len(line), 12)], ansi.StringWidth(line))
					break
				}
			}
		}
	})
}

func TestContinueCommand(t *testing.T) {
	t.Run("TU-11 after esc the note offers /continue, which starts a turn; not while one runs", func(t *testing.T) {
		m := conversationModel()
		m.session.Agent.History = []llm.Message{{Role: llm.RoleUser, Text: "go"}}
		m.running, m.cancel = true, func() {}
		stopped, cmd := m.finishRun(context.Canceled)
		note := ""
		if cmd != nil {
			note = sent(cmd)
		}

		busy, _ := m.runCommand("/continue")
		resumed, _ := stopped.(model).runCommand("/continue")

		if !strings.Contains(note, "/continue goes on") || !resumed.(model).running || strings.Contains(shown(busy.(model)), "continuing") {
			t.Errorf("note = %q resumed running = %v", note, resumed.(model).running)
		}
	})
}

func TestWhileRunning(t *testing.T) {
	typed := func(m model, text string) model {
		m.input.SetValue(text)
		updated, _ := m.Update(keyEnter)
		return updated.(model)
	}

	t.Run("TU-12 written while the model works, a message is queued; esc gives it back", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}

		m = typed(m, "also check the tests")
		queued := m.session.Agent.HasQueued()
		stopped, _ := m.finishRun(context.Canceled)

		if !queued || stopped.(model).input.Value() != "also check the tests" || stopped.(model).session.Agent.HasQueued() {
			t.Errorf("queued = %v input = %q", queued, stopped.(model).input.Value())
		}
	})

	t.Run("TU-13 safe commands work while the model works; saved settings take over at the turn's end", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}

		cleared := typed(m, "/clear")
		sidebar := typed(m, "/sidebar")
		fresh := Session{Agent: &agent.Agent{}, Provider: "fresh", Config: config.Defaults()}
		fresh.Config.Render.Keys = "vim"
		waiting, _ := m.finishSettings(settingsSavedMsg{session: fresh, saved: []string{"config.toml"}, waiting: true})
		finished, _ := waiting.(model).finishRun(nil)

		if !strings.Contains(shown(cleared), "wait for the model to finish") || !sidebar.sidebarHidden {
			t.Errorf("clear:\n%s", shown(cleared))
		}
		if waiting.(model).pendingSession == nil || waiting.(model).session.Provider == "fresh" {
			t.Error("the new session should wait for the turn's end")
		}
		if done := finished.(model); done.pendingSession != nil || done.session.Provider != "fresh" || !done.vim.enabled {
			t.Errorf("after the turn: provider = %q vim = %v", done.session.Provider, done.vim.enabled)
		}
	})
}

func TestInputLines(t *testing.T) {
	t.Run("the input takes any number of lines and grows to ten, scrolled inside past them", func(t *testing.T) {
		m := conversationModel()
		m.width, m.height = 100, 40
		for i := range 25 {
			if i > 0 {
				updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
				m = updated.(model)
			}
			updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			m = updated.(model)
		}

		if lines := strings.Count(m.input.Value(), "\n") + 1; lines != 25 {
			t.Errorf("lines = %d", lines)
		}
		if height := lipgloss.Height(m.input.View()); height > 10 {
			t.Errorf("height = %d", height)
		}
	})
}

func TestCommandOutputSetting(t *testing.T) {
	t.Run("the setting of where command output is kept reaches the jobs with the session", func(t *testing.T) {
		m := sidebarModel(t, 140)
		session := m.session
		session.Config = config.Defaults()
		session.Config.Tools.Outputs = "none"

		updated, _ := m.useSession(session)

		if updated.(model).jobs().Kept() {
			t.Error("with outputs none the jobs should keep nothing")
		}
	})
}
