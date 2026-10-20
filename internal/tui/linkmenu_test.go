package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | LK-01 | Happy    | A right click on a markdown link's words offers to open it, copy the link and copy those words | P0 |
// | LK-02 | Happy    | A right click on an address an answer wrote offers to open and copy it, no text to copy | P0 |
// | LK-03 | Happy    | An address in plain text, as a command prints it, is found without its trailing punctuation | P1 |
// | LK-04 | Happy    | Copy the link puts the address on the clipboard and says so                 | P1       |
// | LK-05 | Edge     | Words that are no link, beside one, open no link menu                       | P1       |
// | LK-06 | Edge     | OSC 8 closed with ESC \ is read as well as with BEL; other escapes are skipped | P2    |

import (
	"slices"
	"strings"
	"testing"
)

func TestLinkMenu(t *testing.T) {
	answered := func(t *testing.T, text string) model {
		t.Helper()
		m, _ := fileModel(t)
		updated, _ := m.Update(drawMsg(func() string { return m.printer.Answer(text) }))
		return updated.(model)
	}

	t.Run("LK-01 a right click on a markdown link's words offers to open it, copy the link and copy those words", func(t *testing.T) {
		m := answered(t, "read [the guide](https://go.dev/doc/effective_go) first")

		m = rightClickOn(t, m, "the guide")

		if m.popup == nil {
			t.Fatal("no menu")
		}
		if values := optionValues(m); !slices.Equal(values, []string{linkOpen, linkCopy, linkCopyText}) || m.popup.description != "https://go.dev/doc/effective_go" {
			t.Errorf("options %v description %q", values, m.popup.description)
		}
		if words := m.popup.options[2].Key; words != "copy its text: the guide" {
			t.Errorf("words option %q", words)
		}
	})

	t.Run("LK-02 a right click on an address an answer wrote offers to open and copy it, no text to copy", func(t *testing.T) {
		m := answered(t, "see https://example.com/a?b=1 for more")

		m = rightClickOn(t, m, "example.com")

		if m.popup == nil || !slices.Equal(optionValues(m), []string{linkOpen, linkCopy}) || m.popup.description != "https://example.com/a?b=1" {
			t.Errorf("popup %+v", m.popup)
		}
	})

	t.Run("LK-03 an address in plain text, as a command prints it, is found without its trailing punctuation", func(t *testing.T) {
		found, ok := linkAt("Listening on (http://localhost:8080/api).", 20)

		if !ok || found.url != "http://localhost:8080/api" || found.text != found.url {
			t.Errorf("found %+v ok %v", found, ok)
		}
	})

	t.Run("LK-04 copy the link puts the address on the clipboard and says so", func(t *testing.T) {
		m := answered(t, "see https://example.com/x")
		m = rightClickOn(t, m, "example.com")

		chosen, cmd := choose(t, m, linkCopy)

		if cmd == nil || !strings.Contains(chosen.flash, "copied the link") {
			t.Errorf("flash %q", chosen.flash)
		}
	})

	t.Run("LK-05 words that are no link, beside one, open no link menu", func(t *testing.T) {
		line := "\x1b]8;;https://a.b/c\athe link\x1b]8;;\a and more words"

		_, onWords := linkAt(line, 12)
		onLink, onIt := linkAt(line, 2)

		if onWords || !onIt || onLink.url != "https://a.b/c" || onLink.text != "the link" {
			t.Errorf("on words %v, on link %+v %v", onWords, onLink, onIt)
		}
	})

	t.Run("LK-06 OSC 8 closed with ESC \\ is read as well as with BEL; other escapes are skipped", func(t *testing.T) {
		line := "\x1b[1mgo \x1b]8;id=7;https://go.dev\x1b\\\x1b[4mhere\x1b[m\x1b]8;;\x1b\\ now"

		found, ok := linkAt(line, 4)
		_, before := linkAt(line, 1)

		if !ok || found.url != "https://go.dev" || found.text != "here" || before {
			t.Errorf("found %+v ok %v before %v", found, ok, before)
		}
	})
}
