package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VT-01 | Happy    | /view opens a tree of the folder, folders first, and no file yet         | P0       |
// | VT-02 | Happy    | → opens a folder, ← closes it or goes to the one over a file             | P0       |
// | VT-03 | Happy    | enter on a file opens it beside the tree, which is kept and loses focus   | P0       |
// | VT-04 | Happy    | → on a file shows it and keeps the focus on the tree                     | P1       |
// | VT-05 | Happy    | tab goes between the tree and the file; ↑↓ move what has the focus        | P0       |
// | VT-06 | Happy    | / narrows the tree to the files that hold what was typed, a pattern too   | P0       |
// | VT-07 | Happy    | t opens a tree beside a file, on that file, and hides it again            | P0       |
// | VT-08 | Edge     | Tree and file fit the screen: no line is wider than it                    | P0       |
// | VT-09 | Happy    | A click on a row of the tree acts as enter or →; one on the text gives it the focus | P1 |
// | VT-10 | Edge     | With no file the keys of a job do nothing, and esc closes                 | P0       |
// | VT-11 | Edge     | A file over the root, a picture, and a terminal too narrow are handled   | P1       |
// | VT-12 | Edge     | A long tree scrolls to keep the cursor in view                           | P1       |
// | VT-14 | Happy    | Each file name has its kind: the language, or the sort of thing it is     | P0       |
// | VT-15 | Happy    | Nerd icons: a folder and each kind of file have theirs, in the colour of the language | P0 |
// | VT-16 | Happy    | Emoji icons: the same as emoji, rows keep their width                    | P0       |
// | VT-17 | Edge     | Text: ▸ ▾ and a / after a folder, no icon for a file; the status line takes emoji as text | P1 |
// | VT-18 | Edge     | Every kind has an icon in both sets, and the open and closed folder differ | P1      |
// | VT-13 | Edge     | Made narrower the tree is hidden and the file takes the screen; wider, it is back | P1 |

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func letter(text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(text[0]), Text: text} }

var (
	keyUpMsg     = tea.KeyPressMsg{Code: tea.KeyUp}
	keyRightMsg  = tea.KeyPressMsg{Code: tea.KeyRight}
	keyLeftMsg   = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyEnterTree = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyTabMsg    = tea.KeyPressMsg{Code: tea.KeyTab}
)

func explorer(t *testing.T) model {
	t.Helper()
	m := searchModel(t)
	opened, _ := m.runCommand("/view")
	return opened.(model)
}

func rowNames(m model) []string {
	var names []string
	for _, row := range m.viewer.tree.rows {
		name := row.name
		if row.dir {
			name += "/"
		}
		names = append(names, name)
	}
	return names
}

func seen(m model) string { return ansi.Strip(m.View().Content) }

func TestViewTree(t *testing.T) {
	t.Run("VT-01 /view opens a tree of the folder, folders first, and no file yet", func(t *testing.T) {
		m := explorer(t)

		names := strings.Join(rowNames(m), ",")
		plain := seen(m)

		if names != ".hidden/,docs/,node_modules/,src/,vendor/,mi_file.go,other.txt" {
			t.Errorf("rows %s", names)
		}
		for _, want := range []string{"▸ docs/", "▸ src/", "mi_file.go", "choose a file in the tree", "Files"} {
			if !strings.Contains(plain, want) {
				t.Errorf("the screen lacks %q:\n%s", want, plain)
			}
		}
	})

	t.Run("VT-02 → opens a folder, ← closes it or goes to the one over a file", func(t *testing.T) {
		m := explorer(t)
		m = keys(t, m, keyDown, keyDown, keyDown)
		if row, _ := m.viewer.tree.selected(); row.name != "src" {
			t.Fatalf("on %s", row.name)
		}

		m = keys(t, m, keyRightMsg)
		src := strings.Join(rowNames(m), ",")
		m = keys(t, m, keyDown, keyDown, keyRightMsg, keyDown)
		inside, _ := m.viewer.tree.selected()
		m = keys(t, m, keyLeftMsg)
		up, _ := m.viewer.tree.selected()
		m = keys(t, m, keyLeftMsg)
		closed := strings.Join(rowNames(m), ",")

		if !strings.Contains(src, "src/,deep/,pkg/,vendor/") {
			t.Errorf("src opened: %s", src)
		}
		if inside.name != "mi_file.txt" || up.name != "pkg" || strings.Contains(closed, "mi_file.txt") || !strings.Contains(closed, "pkg/") {
			t.Errorf("inside %q up %q closed %s", inside.name, up.name, closed)
		}
	})

	t.Run("VT-03 enter on a file opens it beside the tree, which is kept and loses focus", func(t *testing.T) {
		m := explorer(t)
		tree := m.viewer.tree
		m = keys(t, m, keyDown, keyDown, keyDown, keyDown, keyDown)
		if row, _ := m.viewer.tree.selected(); row.name != "mi_file.go" {
			t.Fatalf("on %s", row.name)
		}

		m = keys(t, m, keyEnterTree)
		plain := seen(m)

		v := m.viewer
		if v.file == "" || v.tree != tree || v.treeFocus || v.placeholder || !strings.Contains(v.file, "mi_file.go") {
			t.Fatalf("viewer %+v", v)
		}
		if !strings.Contains(plain, "▸ docs/") || !strings.Contains(plain, "package main") || !strings.Contains(plain, "only you see it") {
			t.Errorf("tree and file side by side:\n%s", plain)
		}
		if tree.current != filepath.Join(m.session.WorkDir, "mi_file.go") {
			t.Errorf("the tree marks %q", tree.current)
		}
	})

	t.Run("VT-04 → on a file shows it and keeps the focus on the tree", func(t *testing.T) {
		m := explorer(t)

		m = keys(t, m, keyDown, keyDown, keyDown, keyDown, keyDown, keyRightMsg)

		if m.viewer.file == "" || !m.viewer.treeFocus {
			t.Errorf("file %q focus %v", m.viewer.file, m.viewer.treeFocus)
		}
	})

	t.Run("VT-05 tab goes between the tree and the file; ↑↓ move what has the focus", func(t *testing.T) {
		m := explorer(t)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "a_multi.txt"), []byte("one\ntwo\nthree\n"), 0o644)
		m.viewer.tree.rebuild()
		m = keys(t, m, keyDown, keyDown, keyDown, keyDown, keyDown, keyEnterTree)
		treeAt, _ := m.viewer.tree.selected()
		if treeAt.name != "a_multi.txt" {
			t.Fatalf("on %s", treeAt.name)
		}

		m = keys(t, m, keyDown)
		fileMoved := m.viewer.cursor
		stillAt, _ := m.viewer.tree.selected()
		m = keys(t, m, keyTabMsg)
		back := m.viewer.treeFocus
		m = keys(t, m, keyUpMsg)
		movedUp, _ := m.viewer.tree.selected()
		m = keys(t, m, keyTabMsg)

		if fileMoved != 1 || stillAt.path != treeAt.path || !back || movedUp.path == treeAt.path || m.viewer.treeFocus {
			t.Errorf("file cursor %d, tree %q→%q→%q, focus %v", fileMoved, treeAt.name, stillAt.name, movedUp.name, m.viewer.treeFocus)
		}
	})

	t.Run("VT-06 / narrows the tree to the files that hold what was typed, a pattern too", func(t *testing.T) {
		m := explorer(t)

		m = keys(t, m, letter("/"))
		typing := seen(m)
		for _, c := range "mi_file" {
			m = keys(t, m, letter(string(c)))
		}
		m = keys(t, m, keyEnterTree)
		found := strings.Join(rowNames(m), ",")
		m = keys(t, m, keyEnterTree)
		opened := m.viewer.file
		m = keys(t, m, keyTabMsg, tea.KeyPressMsg{Code: tea.KeyEscape})
		cleared := len(m.viewer.tree.rows)
		m = keys(t, m, letter("/"))
		for _, c := range "**/*.md" {
			m = keys(t, m, letter(string(c)))
		}
		m = keys(t, m, keyEnterTree)

		if !strings.Contains(typing, "enter find") || found != "mi_file.go,docs/MI_FILE.md,src/pkg/mi_file.txt,src/deep/er/mi_file.test.go" {
			t.Errorf("typing: %q found %s", typing, found)
		}
		if !strings.HasSuffix(opened, "mi_file.go") || cleared != 7 {
			t.Errorf("opened %q, rows after clearing %d", opened, cleared)
		}
		if got := strings.Join(rowNames(m), ","); got != "docs/MI_FILE.md,src/pkg/readme.md" {
			t.Errorf("**/*.md found %s", got)
		}
	})

	t.Run("VT-07 t opens a tree beside a file, on that file, and hides it again", func(t *testing.T) {
		m := searchModel(t)
		opened, _ := m.runCommand("/view src/pkg/readme.md")
		m = opened.(model)
		if m.viewer.tree != nil {
			t.Fatal("a file opens with no tree")
		}

		m = keys(t, m, letter("t"))
		row, _ := m.viewer.tree.selected()
		plain := seen(m)
		m = keys(t, m, keyTabMsg, letter("t"))

		if m.viewer.tree != nil || row.name != "readme.md" || !m.viewer.treeFocus == false && m.viewer.tree != nil {
			t.Errorf("selected %q, tree after hiding %v", row.name, m.viewer.tree)
		}
		if !strings.Contains(plain, "▾ src/") || !strings.Contains(plain, "▾ pkg/") || !strings.Contains(plain, "mi_file.txt") {
			t.Errorf("revealed:\n%s", plain)
		}
	})

	t.Run("VT-08 tree and file fit the screen: no line is wider than it", func(t *testing.T) {
		m := explorer(t)
		long := strings.Repeat("x", 300)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "wide.txt"), []byte(long+"\n"+long), 0o644)
		m.viewer.tree.rebuild()
		for range 20 {
			m = keys(t, m, keyDown)
		}
		for _, width := range []int{60, 80, 100, 160} {
			m.width = width
			m = keys(t, m, keyEnterTree)
			for i, line := range strings.Split(m.View().Content, "\n") {
				if shown := ansi.StringWidth(line); shown > width {
					t.Fatalf("width %d: line %d is %d wide: %q", width, i, shown, ansi.Strip(line))
				}
			}
		}
		if m.viewerWidth() != 160-m.treeWidth()-1 {
			t.Errorf("the text has %d columns", m.viewerWidth())
		}
	})

	t.Run("VT-09 a click on a row of the tree acts as enter or →; one on the text gives it the focus", func(t *testing.T) {
		m := explorer(t)

		clicked, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: 2})
		m = clicked.(model)
		docsOpen := strings.Join(rowNames(m), ",")
		clicked, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: 2})
		m = clicked.(model)
		docsShut := strings.Join(rowNames(m), ",")
		clicked, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: 6})
		m = clicked.(model)
		shown := m.viewer.file
		clicked, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 60, Y: 3})
		m = clicked.(model)

		if !strings.Contains(docsOpen, "MI_FILE.md") || strings.Contains(docsShut, "MI_FILE.md") {
			t.Errorf("a folder: %s | %s", docsOpen, docsShut)
		}
		if !strings.HasSuffix(shown, "mi_file.go") || m.viewer.treeFocus {
			t.Errorf("file %q, tree focus after a click on the text: %v", shown, m.viewer.treeFocus)
		}
	})

	t.Run("VT-10 with no file the keys of a job do nothing, and esc closes", func(t *testing.T) {
		m := explorer(t)
		m = keys(t, m, keyTabMsg)
		for _, key := range []string{"x", "b", "y", "n", "c", "a", "s"} {
			m = keys(t, m, letter(key))
		}
		if m.viewer == nil || !m.viewer.placeholder || m.viewer.notice != "" {
			t.Fatalf("viewer %+v", m.viewer)
		}
		m = keys(t, m, keyTabMsg)
		if !m.viewer.treeFocus {
			t.Error("tab should give the tree the focus back")
		}
		m = keys(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.viewer != nil {
			t.Error("esc should close")
		}
	})

	t.Run("VT-11 a file over the root, a picture, and a terminal too narrow are handled", func(t *testing.T) {
		m := explorer(t)
		tree := m.viewer.tree
		other := filepath.Join(t.TempDir(), "elsewhere.txt")
		tree.reveal(other, 10)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "pic.png"), []byte("not really"), 0o644)
		tree.rebuild()
		for i, row := range tree.rows {
			if row.name == "pic.png" {
				tree.cursor = i
			}
		}

		m = keys(t, m, keyEnterTree)

		if tree.cursor == 0 && tree.current != "" {
			t.Error("a file outside the root moved the tree")
		}
		if !m.viewer.placeholder || !strings.Contains(m.viewer.notice, "show in the conversation") {
			t.Errorf("a picture: placeholder %v notice %q", m.viewer.placeholder, m.viewer.notice)
		}
		narrow := searchModel(t)
		opened, _ := narrow.runCommand("/view main_missing_nothing")
		_ = opened
		file, _ := narrow.runCommand("/view other.txt")
		narrow = file.(model)
		narrow.width = 50
		narrow = keys(t, narrow, letter("t"))
		if narrow.viewer.tree != nil || !strings.Contains(narrow.viewer.notice, "too narrow") {
			t.Errorf("narrow: tree %v notice %q", narrow.viewer.tree, narrow.viewer.notice)
		}
	})

	t.Run("VT-12 a long tree scrolls to keep the cursor in view", func(t *testing.T) {
		m := explorer(t)
		for i := range 80 {
			_ = os.WriteFile(filepath.Join(m.session.WorkDir, fmt.Sprintf("z%03d.txt", i)), []byte("x"), 0o644)
		}
		m.viewer.tree.rebuild()

		for range 60 {
			m = keys(t, m, keyDown)
		}
		plain := seen(m)
		row, _ := m.viewer.tree.selected()
		m = keys(t, m, letter("g"))

		if !strings.Contains(plain, row.name) || m.viewer.tree.top != 0 || m.viewer.tree.cursor != 0 {
			t.Errorf("cursor on %s, top %d:\n%s", row.name, m.viewer.tree.top, plain)
		}
	})

	t.Run("VT-13 made narrower the tree is hidden and the file takes the screen; wider, it is back", func(t *testing.T) {
		m := explorer(t)
		m = keys(t, m, keyDown, keyDown, keyDown, keyDown, keyDown, keyEnterTree)

		m.width = 50
		narrow := m.View().Content
		kept := m.viewer.tree != nil
		m.width = 100
		wide := ansi.Strip(m.View().Content)

		for i, line := range strings.Split(narrow, "\n") {
			if ansi.StringWidth(line) > 50 {
				t.Fatalf("line %d is %d wide", i, ansi.StringWidth(line))
			}
		}
		if !kept || strings.Contains(ansi.Strip(narrow), "▸ docs/") || !strings.Contains(wide, "▸ docs/") || !strings.Contains(wide, "package main") {
			t.Errorf("narrow:\n%s\nwide:\n%s", ansi.Strip(narrow), wide)
		}
	})

	t.Run("VT-14 each file name has its kind: the language, or the sort of thing it is", func(t *testing.T) {
		cases := map[string]string{
			"main.go": "go", "App.PY": "python", "index.jsx": "javascript", "app.tsx": "typescript", "lib.rs": "rust",
			"x.h": "c", "x.hpp": "cpp", "A.java": "java", "init.lua": "lua", "run.sh": "shell", "README.md": "markdown",
			"data.json": "json", "ci.yml": "yaml", "mo.toml": "config", "Makefile": "config", "page.html": "html",
			"site.scss": "css", "query.sql": "sql", "photo.JPG": "image", "doc.pdf": "pdf", "all.tar.gz": "archive",
			"notes.txt": "text", "Dockerfile": "docker", ".gitignore": "git", "go.sum": "lock", "package-lock.json": "lock",
			"unknown.xyz": "file", "noextension": "file",
		}
		for name, want := range cases {
			if got := fileKind(name); got != want {
				t.Errorf("%s is %q, want %q", name, got, want)
			}
		}
	})

	t.Run("VT-15 nerd icons: a folder and each kind of file have theirs, in the colour of the language", func(t *testing.T) {
		m := explorer(t)
		m.session.Config.Render.Icons = iconsNerd
		m = keys(t, m, keyDown, keyDown, keyDown, keyRightMsg)
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "src", "a.go"), []byte("package a"), 0o644)
		m.viewer.tree.rebuild()

		drawn := m.View().Content
		plain := ansi.Strip(drawn)

		for _, want := range []string{"\uf07b docs", "\uf07c src", "\ue627 a.go", "\ue627 mi_file.go", "\uf15c other.txt"} {
			if !strings.Contains(plain, want) {
				t.Errorf("the tree lacks %q:\n%s", want, plain)
			}
		}
		if strings.Contains(plain, "▸") || strings.Contains(plain, "docs/") {
			t.Errorf("nerd icons come in place of the markers and the /:\n%s", plain)
		}
		goRow := ""
		for _, line := range strings.Split(drawn, "\n") {
			if strings.Contains(ansi.Strip(line), "mi_file.go") {
				goRow = line
			}
		}
		if !strings.Contains(goRow, "38;2;0;173;216m\ue627") {
			t.Errorf("the Go icon is not in its colour: %q", goRow)
		}
	})

	t.Run("VT-16 emoji icons: the same as emoji, rows keep their width", func(t *testing.T) {
		m := explorer(t)
		m.session.Config.Render.Icons = iconsEmoji
		m = keys(t, m, keyDown, keyDown, keyDown, keyRightMsg)

		drawn := m.View().Content
		plain := ansi.Strip(drawn)

		for _, want := range []string{"📁 docs", "📂 src", "🐹 mi_file.go", "📝 other.txt", "📂 src"} {
			if !strings.Contains(plain, want) {
				t.Errorf("the tree lacks %q:\n%s", want, plain)
			}
		}
		for i, line := range strings.Split(drawn, "\n") {
			if shown := ansi.StringWidth(line); shown > m.width {
				t.Fatalf("line %d is %d wide: %q", i, shown, ansi.Strip(line))
			}
		}
	})

	t.Run("VT-17 text: ▸ ▾ and a / after a folder, no icon for a file; the status line takes emoji as text", func(t *testing.T) {
		m := explorer(t)
		m = keys(t, m, keyDown, keyDown, keyDown, keyRightMsg)

		plain := seen(m)

		for _, want := range []string{"▸ docs/", "▾ src/", "  mi_file.go"} {
			if !strings.Contains(plain, want) {
				t.Errorf("the tree lacks %q:\n%s", want, plain)
			}
		}
		for set, want := range map[string]string{iconsNerd: iconsNerd, iconsText: iconsText, iconsEmoji: iconsText, "": iconsNerd} {
			m.session.Config.Render.Icons = set
			if m.icons() != want {
				t.Errorf("the status line with %q has %q icons, want %q", set, m.icons(), want)
			}
		}
	})

	t.Run("VT-18 every kind has an icon in both sets, and the open and closed folder differ", func(t *testing.T) {
		kinds := []string{"folder", "folderOpen", "file", "text", "go", "python", "javascript", "typescript", "rust", "c", "cpp", "java", "lua",
			"shell", "markdown", "json", "yaml", "config", "html", "css", "docker", "git", "sql", "image", "pdf", "archive", "lock"}
		for _, kind := range kinds {
			nerd, hasNerd := nerdIcons[kind]
			emoji, hasEmoji := emojiIcons[kind]
			if !hasNerd || nerd.glyph == "" || nerd.color == nil || !hasEmoji || emoji == "" {
				t.Errorf("%s: nerd %+v emoji %q", kind, nerd, emoji)
			}
		}
		closed, _ := iconOf(iconsNerd, "src", true, false)
		open, _ := iconOf(iconsNerd, "src", true, true)
		if closed.glyph == open.glyph {
			t.Error("a folder open looks as one closed")
		}
		if _, any := iconOf(iconsText, "main.go", false, false); any {
			t.Error("text has no icons")
		}
		// Every kind fileKind can say has an icon.
		for _, name := range []string{"a.go", "a.py", "a.js", "a.ts", "a.rs", "a.c", "a.cpp", "a.java", "a.lua", "a.sh", "a.md", "a.json", "a.yml", "a.toml", "a.html", "a.css", "a.sql", "a.png", "a.pdf", "a.zip", "a.lock", "a.txt", "a.zzz", "Dockerfile", ".gitignore"} {
			if _, found := iconOf(iconsNerd, name, false, false); !found {
				t.Errorf("%s has no nerd icon (kind %s)", name, fileKind(name))
			}
			if _, found := iconOf(iconsEmoji, name, false, false); !found {
				t.Errorf("%s has no emoji (kind %s)", name, fileKind(name))
			}
		}
	})
}
