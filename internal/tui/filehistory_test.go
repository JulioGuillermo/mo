package tui

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | FH-01 | Happy    | B shows who last changed the cursor's line at its end; B again hides it     | P0       |
// | FH-02 | Happy    | H opens the history on the commit that last changed the cursor's line, listed beside it | P0 |
// | FH-03 | Happy    | ctrl+n and ctrl+p go to the older and the newer commit; a click opens one; esc goes back | P0 |
// | FH-04 | Happy    | Changes not committed are the first entry; /git history opens on the newest commit | P0 |
// | FH-05 | Happy    | / in the list keeps the commits that hold what was typed                    | P1       |
// | FH-06 | Happy    | With the blame setting on, a file opens showing it                          | P1       |
// | FH-07 | Happy    | H on a diff of /git show opens that file's history on that commit           | P1       |
// | FH-08 | Happy    | Settings writes the blame setting only when on, off being the default        | P1       |

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/jg/mo/internal/config"
)

// fileHistoryModel is a repository whose src/edit.go three commits changed,
// each with its own subject, opened in the viewer.
func fileHistoryModel(t *testing.T) (model, string) {
	t.Helper()
	m, dir := gitModel(t)
	m.width, m.height = 140, 30
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	commitAs(t, repo, dir, "add b", later, "package src\n\nfunc a() {}\n\nfunc b() {}\n")
	commitAs(t, repo, dir, "change b", later.Add(time.Minute), "package src\n\nfunc a() {}\n\nfunc b() { two() }\n")
	opened, _ := m.openFile("src/edit.go")
	return opened.(model), dir
}

func commitAs(t *testing.T, repo *git.Repository, dir, subject string, when time.Time, content string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "src/edit.go"), content)
	tree, _ := repo.Worktree()
	if _, err := tree.Add("src/edit.go"); err != nil {
		t.Fatal(err)
	}
	signature := &object.Signature{Name: "Ana Gil", Email: "ana@x", When: when}
	if _, err := tree.Commit(subject, &git.CommitOptions{Author: signature, Committer: signature}); err != nil {
		t.Fatal(err)
	}
}

// settled runs a command and what it sends back, as the program would.
func settledCmd(m model, cmd tea.Cmd) model {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m
		}
		updated, next := m.Update(msg)
		m, cmd = updated.(model), next
		if _, batch := msg.(tea.BatchMsg); batch {
			return m
		}
	}
	return m
}

func keyNamed(text string) tea.KeyPressMsg {
	if letter, ctrl := strings.CutPrefix(text, "ctrl+"); ctrl {
		return tea.KeyPressMsg{Code: rune(letter[0]), Mod: tea.ModCtrl}
	}
	switch text {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(text[0]), Text: text}
}

func pressedKeys(m model, keys ...string) model {
	for _, each := range keys {
		updated, cmd := m.Update(keyNamed(each))
		m = settledCmd(updated.(model), cmd)
	}
	return m
}

func viewerScreen(m model) string {
	return ansi.Strip(m.viewerView())
}

func TestFileHistory(t *testing.T) {
	t.Run("FH-01 B shows who last changed the cursor's line at its end; B again hides it", func(t *testing.T) {
		m, _ := fileHistoryModel(t)

		m = pressedKeys(m, "down", "down", "down", "down", "B")
		shown := viewerScreen(m)
		m = pressedKeys(m, "B")

		if !strings.Contains(shown, "func b() { two() }   Ana Gil · ") || !strings.Contains(shown, " change b") {
			t.Errorf("with blame:\n%s", shown)
		}
		if strings.Contains(viewerScreen(m), "Ana Gil") {
			t.Errorf("blame still shown:\n%s", viewerScreen(m))
		}
	})

	t.Run("FH-02 H opens the history on the commit that last changed the cursor's line, listed beside it", func(t *testing.T) {
		m, _ := fileHistoryModel(t)

		m = pressedKeys(m, "down", "down", "H")
		shown := viewerScreen(m)

		if m.viewer.history == nil || !strings.Contains(shown, "history · 3 commits") {
			t.Fatalf("no history:\n%s", shown)
		}
		rows := strings.Split(shown, "\n")
		if !strings.Contains(rows[1], "change b") || !strings.Contains(rows[2], "add b") || !strings.Contains(rows[3], "▶") || !strings.Contains(rows[3], "files") {
			t.Errorf("list:\n%s", shown)
		}
		if !strings.Contains(rows[0], "3 of 3") || !strings.Contains(shown, "+ func a() {}") {
			t.Errorf("diff:\n%s", shown)
		}
	})

	t.Run("FH-03 ctrl+n and ctrl+p go to the older and the newer commit; a click opens one; esc goes back", func(t *testing.T) {
		m, _ := fileHistoryModel(t)
		file := m.viewer
		m = pressedKeys(m, "down", "down", "down", "down", "H")

		newer := pressedKeys(m, "ctrl+p")
		header := strings.Split(viewerScreen(newer), "\n")[0]
		older := pressedKeys(newer, "ctrl+n")
		clicked, _ := older.Update(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft})
		back := pressedKeys(clicked.(model), "esc")

		if !strings.Contains(header, "1 of 3") || !strings.Contains(header, "change b") {
			t.Errorf("after ctrl+p: %s", header)
		}
		if !strings.Contains(strings.Split(viewerScreen(older), "\n")[0], "2 of 3") {
			t.Errorf("after ctrl+n: %s", strings.Split(viewerScreen(older), "\n")[0])
		}
		if !strings.Contains(strings.Split(viewerScreen(clicked.(model)), "\n")[0], "add b") {
			t.Errorf("after the click: %s", strings.Split(viewerScreen(clicked.(model)), "\n")[0])
		}
		if back.viewer != file {
			t.Error("esc does not go back to the file")
		}
	})

	t.Run("FH-04 changes not committed are the first entry; /git history opens on the newest commit", func(t *testing.T) {
		m, dir := fileHistoryModel(t)
		writeFile(t, filepath.Join(dir, "src/edit.go"), "package src\n\nfunc a() { mine() }\n\nfunc b() { two() }\n")
		m.viewer = nil

		updated, cmd := m.runCommand("/git history src/edit.go")
		m = settledCmd(updated.(model), cmd)
		rows := strings.Split(viewerScreen(m), "\n")

		if !strings.Contains(rows[0], "history · 3 commits") || !strings.Contains(rows[1], "● not committed yet") || !strings.Contains(rows[2], "▶") {
			t.Errorf("list:\n%s", strings.Join(rows, "\n"))
		}
		if !strings.Contains(rows[0], "2 of 4") || !strings.Contains(rows[0], "change b") {
			t.Errorf("header: %s", rows[0])
		}
	})

	t.Run("FH-05 / in the list keeps the commits that hold what was typed", func(t *testing.T) {
		m, _ := fileHistoryModel(t)
		m = pressedKeys(m, "H", "tab", "/", "a", "d", "d", "enter")

		shown := viewerScreen(m)

		if !strings.Contains(shown, "/add · 1 found") || strings.Contains(shown, "change b") {
			t.Errorf("filtered:\n%s", shown)
		}
	})

	t.Run("FH-06 with the blame setting on, a file opens showing it", func(t *testing.T) {
		m, _ := fileHistoryModel(t)
		on := true
		m.session.Config.Render.Blame = &on

		opened, cmd := m.openFile("src/edit.go")
		m = settledCmd(opened.(model), cmd)

		if !strings.Contains(viewerScreen(m), "package src   Ana") {
			t.Errorf("screen:\n%s", viewerScreen(m))
		}
	})

	t.Run("FH-07 H on a diff of /git show opens that file's history on that commit", func(t *testing.T) {
		m, _ := fileHistoryModel(t)
		m.viewer = nil
		m = runGit(t, m, "show HEAD~1")
		m.viewer.treeFocus = false

		m = pressedKeys(m, "H")

		if header := strings.Split(viewerScreen(m), "\n")[0]; !strings.Contains(header, "2 of 3") || !strings.Contains(header, "add b") {
			t.Errorf("header: %s", header)
		}
	})

	t.Run("FH-08 settings writes the blame setting only when on, off being the default", func(t *testing.T) {
		base := config.Config{Provider: "llama-server"}
		draft := newSettingsDraft(base)
		off, _ := draft.applyTo(base, nil)
		draft.blame = true
		on, err := draft.applyTo(base, nil)

		if off.Render.Blame != nil || on.Render.Blame == nil || !*on.Render.Blame || err != nil {
			t.Errorf("off = %v on = %v err = %v", off.Render.Blame, on.Render.Blame, err)
		}
	})
}
