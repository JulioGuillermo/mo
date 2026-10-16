package tui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFitLines(t *testing.T) {
	t.Run("a line too wide wraps again, its quote bar and list indent on every line", func(t *testing.T) {
		quote := "  │ Show the user an interface of panels, cards and labels"
		item := "  │ • a list item that goes on well past the width"

		quoted := strings.Split(fitLines(quote, 30), "\n")
		listed := strings.Split(fitLines(item, 30), "\n")

		for _, line := range append(quoted, listed...) {
			if ansi.StringWidth(line) > 30 {
				t.Errorf("too wide: %q", line)
			}
		}
		if len(quoted) < 2 || !strings.HasPrefix(quoted[1], "  │ ") || strings.HasPrefix(quoted[1], "  │  ") {
			t.Errorf("quoted = %q", quoted)
		}
		if len(listed) < 2 || !strings.HasPrefix(listed[1], "  │   ") || strings.Contains(listed[1], "•") {
			t.Errorf("listed = %q", listed)
		}
		if fitLines("short", 30) != "short" {
			t.Error("a line that fits stays as it is")
		}
	})
}

func TestImageCaption(t *testing.T) {
	t.Run("an image an answer shows has its name and path under it; one without a name, its path; a missing one, no caption", func(t *testing.T) {
		dir := t.TempDir()
		picture := image.NewRGBA(image.Rect(0, 0, 4, 4))
		file, _ := os.Create(filepath.Join(dir, "shot.png"))
		_ = png.Encode(file, picture)
		file.Close()
		p := newPrinter()
		p.workDir = dir

		named := ansi.Strip(p.Answer("Here:\n\n![Login screen](shot.png)\n"))
		bare := ansi.Strip(p.Answer("![](shot.png)"))
		missing := ansi.Strip(p.Answer("![Gone](missing.png)"))

		lines := strings.Split(strings.TrimRight(named, "\n"), "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "Login screen · shot.png" || !strings.Contains(named, "[image 4x4]") {
			t.Errorf("named:\n%s", named)
		}
		bareLines := strings.Split(strings.TrimRight(bare, "\n"), "\n")
		if strings.TrimSpace(bareLines[len(bareLines)-1]) != "shot.png" {
			t.Errorf("bare:\n%s", bare)
		}
		if strings.Contains(missing, "Gone ·") {
			t.Errorf("a missing image should have no caption:\n%s", missing)
		}
	})
}

func TestSpacing(t *testing.T) {
	t.Run("a drawing stands apart from the text around it by one blank line, the line under it with it", func(t *testing.T) {
		p := newPrinter()
		p.setWidth(40)
		wide := "```mermaid\ngraph LR\n  Alpha[Alpha box] --> Beta[Beta box] --> Gamma[Gamma box] --> Delta[Delta box]\n```"
		lines := strings.Split(ansi.Strip(p.Answer("Before.\n"+wide+"\nAfter.")), "\n")
		var kinds []string
		for _, line := range lines {
			switch trimmed := strings.TrimSpace(line); {
			case trimmed == "":
				kinds = append(kinds, "blank")
			case strings.HasPrefix(trimmed, "Before") || strings.HasPrefix(trimmed, "After"):
				kinds = append(kinds, "text")
			case strings.Contains(trimmed, "out of view"):
				kinds = append(kinds, "hint")
			default:
				kinds = append(kinds, "drawing")
			}
		}
		if got := strings.Join(kinds, " "); got != "blank text blank drawing drawing drawing hint blank text" {
			t.Errorf("kinds = %s\n%s", got, strings.Join(lines, "\n"))
		}
	})

	t.Run("mo's notes are marked and stand apart from what came before", func(t *testing.T) {
		m := sidebarModel(t, 100)
		updated, _ := m.Update(printMsg(m.printer.Answer("The model says this.")))
		updated, _ = updated.(model).Update(printMsg(m.printer.Note("resumed: a talk · 3 prompts\nsecond line")))
		content := ansi.Strip(updated.(model).conversationContent())

		if !strings.Contains(content, "says this.") || !strings.Contains(content, "\n\n◇ resumed: a talk · 3 prompts\n  second line") {
			t.Errorf("content:\n%s", content)
		}
	})
}

func TestTerminalText(t *testing.T) {
	t.Run("a command's output is drawn as a terminal leaves it: \\r\\n a new line, \\r over its line, no controls", func(t *testing.T) {
		output := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nAccess-Control-Allow-Headers: Authorization, Accept\r\n\r\n" +
			"downloading  10%\rdownloading  50%\rdownloading 100%\n\x1b[32mdone\x1b[0m\a\n\tindented"
		p := newPrinter()
		drawn := p.fullResult(output, false) + "\n" + p.ToolResult(output, false)

		if strings.ContainsAny(ansi.Strip(drawn), "\r\a\b") {
			t.Errorf("controls drawn: %q", ansi.Strip(drawn))
		}
		plain := ansi.Strip(drawn)
		for _, want := range []string{"HTTP/1.1 200 OK", "Access-Control-Allow-Headers: Authorization, Accept", "downloading 100%", "done"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%q not shown:\n%s", want, plain)
			}
		}
		if strings.Contains(plain, "downloading  10%") || terminalText("a\r") != "a" || terminalText("plain\ttext") != "plain\ttext" {
			t.Errorf("a \\r should leave its last text: %q", plain)
		}
	})
}
