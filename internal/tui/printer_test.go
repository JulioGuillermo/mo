package tui

import (
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
