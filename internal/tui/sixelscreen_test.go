package tui

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | SS-01 | Happy    | A frame's mark becomes its strips, each at its cell, the cursor kept   | P0       |
// | SS-02 | Happy    | The same frame again draws nothing: its strips are on screen           | P0       |
// | SS-03 | Happy    | A strip moved, as by a scroll, is drawn at its new cell                | P0       |
// | SS-04 | Edge     | A screen erased draws again every strip                                | P1       |
// | SS-05 | Edge     | Output with no mark goes out as it is                                  | P1       |
// | SS-06 | Edge     | With no screen a frame gets no mark                                    | P2       |
// | SS-07 | Happy    | auto takes sixel in a sixel terminal, unless kitty; a set mode stays   | P0       |
// | SS-08 | Edge     | A frame that scrolls part of the screen is erased and painted whole   | P0       |
// | SS-09 | Edge     | A frame that scrolls with no picture on screen goes out as it is      | P1       |
// | SS-10 | Happy    | A repaint outside a synchronized frame is wrapped in one              | P0       |

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

func sixelPicture(t *testing.T) string {
	t.Helper()
	render.SetCellSize(8, 16)
	t.Cleanup(func() { render.SetCellSize(0, 0) })
	img := image.NewRGBA(image.Rect(0, 0, 16, 32))
	for y := range 32 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{G: uint8(y * 8), A: 255})
		}
	}
	drawn, err := render.Renderer{Mode: render.ModeSixel, Width: 80}.Image(img)
	if err != nil {
		t.Fatal(err)
	}
	return drawn
}

func flushed(screen *sixelScreen, frame string) string {
	return string(screen.draw([]byte("\x1b[?2026h" + screen.mark(frame) + "\x1b[?2026l")))
}

func TestSixelScreen(t *testing.T) {
	t.Run("SS-01 A frame's mark becomes its strips, each at its cell, the cursor kept", func(t *testing.T) {
		screen := newSixelScreen()
		picture := sixelPicture(t)

		output := flushed(screen, "title\n  "+strings.ReplaceAll(picture, "\n", "\n  ")+"\nstatus")

		if strings.Contains(output, "mo-frame") {
			t.Errorf("the mark reached the terminal")
		}
		first, second := strings.Index(output, ansi.CursorPosition(3, 2)+"\x1bP"), strings.Index(output, ansi.CursorPosition(3, 3)+"\x1bP")
		if first < 0 || second < first || !strings.Contains(output, ansi.SaveCursor) || !strings.HasSuffix(output, ansi.RestoreCursor+"\x1b[?2026l") {
			t.Errorf("output %q", output)
		}
	})

	t.Run("SS-02 The same frame again draws nothing: its strips are on screen", func(t *testing.T) {
		screen := newSixelScreen()
		frame := "title\n" + sixelPicture(t) + "\nstatus"
		flushed(screen, frame)

		again := flushed(screen, frame)

		if strings.Contains(again, "\x1bP") {
			t.Errorf("drew again: %q", again)
		}
	})

	t.Run("SS-03 A strip moved, as by a scroll, is drawn at its new cell", func(t *testing.T) {
		screen := newSixelScreen()
		picture := sixelPicture(t)
		flushed(screen, "a\nb\n"+picture+"\nstatus")

		scrolled := flushed(screen, "b\n"+picture+"\nc\nstatus")

		if !strings.Contains(scrolled, ansi.CursorPosition(1, 2)+"\x1bP") || !strings.Contains(scrolled, ansi.CursorPosition(1, 3)+"\x1bP") {
			t.Errorf("output %q", scrolled)
		}
	})

	t.Run("SS-04 A screen erased draws again every strip", func(t *testing.T) {
		screen := newSixelScreen()
		frame := "title\n" + sixelPicture(t) + "\nstatus"
		flushed(screen, frame)

		redrawn := string(screen.draw([]byte(ansi.EraseEntireScreen + screen.mark(frame))))

		if strings.Count(redrawn, "\x1bP") != 2 {
			t.Errorf("output %q", redrawn)
		}
	})

	t.Run("SS-05 Output with no mark goes out as it is", func(t *testing.T) {
		output := []byte("\x1b[2J\x1b]52;c;eA==\x07text")

		if drawn := newSixelScreen().draw(output); string(drawn) != string(output) {
			t.Errorf("got %q", drawn)
		}
	})

	t.Run("SS-06 With no screen a frame gets no mark", func(t *testing.T) {
		var screen *sixelScreen

		if marked := screen.mark("frame"); marked != "frame" {
			t.Errorf("got %q", marked)
		}
	})

	t.Run("SS-07 auto takes sixel in a sixel terminal, unless kitty; a set mode stays", func(t *testing.T) {
		for _, name := range []string{"KITTY_WINDOW_ID", "TERM", "TERM_PROGRAM", "GHOSTTY_RESOURCES_DIR", "TMUX"} {
			t.Setenv(name, "")
		}
		t.Setenv("COLORTERM", "truecolor")
		sixel := imagesFor(render.ModeAuto, true)
		blocks := imagesFor(render.ModeAuto, false)
		chosen := imagesFor(render.ModeBlocks, true)
		t.Setenv("TERM", "xterm-kitty")
		kitty := imagesFor(render.ModeAuto, true)

		if sixel != render.ModeSixel || blocks != render.ModeBlocks || chosen != render.ModeBlocks || kitty != render.ModeKitty {
			t.Errorf("sixel %s, blocks %s, chosen %s, kitty %s", sixel, blocks, chosen, kitty)
		}
	})
	t.Run("SS-08 A frame that scrolls part of the screen is erased and painted whole", func(t *testing.T) {
		screen := newSixelScreen()
		picture := sixelPicture(t)
		flushed(screen, "a\nb\n"+picture+"\nstatus")
		frame := "b\n" + picture + "\nc\nstatus"

		output := string(screen.draw([]byte("\x1b[1;4r\x1b[1S\x1b[r" + screen.mark(frame))))

		repaint := output[strings.Index(output, ansi.EraseEntireScreen):]
		if !strings.Contains(repaint, ansi.CursorPosition(1, 4)+"c") || !strings.Contains(repaint, ansi.CursorPosition(1, 5)+"status") {
			t.Errorf("text not painted again: %q", repaint)
		}
		if strings.Count(repaint, "\x1bP") != 2 || !strings.HasSuffix(output, ansi.RestoreCursor+ansi.ResetModeSynchronizedOutput) {
			t.Errorf("strips not drawn again: %q", repaint)
		}
	})

	t.Run("SS-09 A frame that scrolls with no picture on screen goes out as it is", func(t *testing.T) {
		screen := newSixelScreen()
		sixelPicture(t)
		output := "\x1b[1;4r\x1b[1S\x1b[r"

		drawn := string(screen.draw([]byte(output + screen.mark("a\nb"))))

		if drawn != output+"a\nb" {
			t.Errorf("got %q", drawn)
		}
	})

	t.Run("SS-10 a repaint outside a synchronized frame is wrapped in one", func(t *testing.T) {
		screen := newSixelScreen()
		picture := sixelPicture(t)
		screen.draw([]byte(screen.mark("a\nb\n" + picture + "\nstatus")))

		output := string(screen.draw([]byte("\x1b[1;4r\x1b[1S\x1b[r" + screen.mark("b\n"+picture+"\nc\nstatus"))))

		if !strings.HasPrefix(output, ansi.SetModeSynchronizedOutput) || !strings.HasSuffix(output, ansi.ResetModeSynchronizedOutput) {
			t.Errorf("output %q", output)
		}
	})
}
