package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | VS-01 | Happy    | /view @name offers the files whose name holds it, by the same search as @ | P0      |
// | VS-02 | Happy    | /view with * ? or ** offers the files the pattern names                   | P0       |
// | VS-03 | Edge     | A pattern with no / is matched on names at any depth; capitals do not matter | P1    |
// | VS-04 | Happy    | A word that is no path in the folder offers the files with it in their name | P0     |
// | VS-05 | Happy    | /view of a search shows the one file it finds, or lists the files to pick from | P0  |
// | VS-06 | Happy    | /view with no argument, or a folder, opens its tree; a narrow screen lists it | P0 |
// | VS-07 | Edge     | Hidden folders and dependencies are left out of searches                | P1       |
// | VS-08 | Happy    | Browsing: .. goes up, a folder lists that, a file is viewed             | P0       |
// | VS-09 | Edge     | A search that finds nothing says so                                     | P1       |
// | VS-10 | Happy    | A big file opens at once and is coloured only where the viewer shows it  | P0       |
// | VS-11 | Edge     | The globs: ** across folders, * within one, ? a character                | P1       |
// | VS-12 | Happy    | Navigating the list: a folder lists that, .. goes up, a file opens       | P0       |
// | VS-13 | Happy    | A pattern being typed, as src/*/ or **/mi, offers the files whose paths it starts | P0 |

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func searchModel(t *testing.T) model {
	t.Helper()
	m := conversationModel()
	m.width, m.height = 100, 30
	m.session.Config.Render.Icons = iconsText
	m.session.WorkDir = t.TempDir()
	for name, content := range map[string]string{
		"src/pkg/mi_file.txt":         "text",
		"docs/MI_FILE.md":             "# doc\n",
		"mi_file.go":                  "package main\n",
		"src/deep/er/mi_file.test.go": "package er\n",
		"other.txt":                   "x",
		"node_modules/pkg/mi_file.js": "x",
		".hidden/mi_file":             "x",
		"vendor/lib/mi_file.go":       "x",
		"src/pkg/readme.md":           "r",
	} {
		path := filepath.Join(m.session.WorkDir, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(content), 0o644)
	}
	return m
}

func suggested(m model, typed string) []string {
	m.input.SetValue(typed)
	var names []string
	for _, each := range m.suggestions() {
		names = append(names, strings.TrimPrefix(each.name, "view "))
	}
	return names
}

func TestViewSearch(t *testing.T) {
	t.Run("VS-01 /view @name offers the files whose name holds it, by the same search as @", func(t *testing.T) {
		m := searchModel(t)

		got := suggested(m, "/view @mi_file")

		if len(got) != 4 || got[0] != "mi_file.go" || got[1] != "docs/MI_FILE.md" && got[1] != "src/pkg/mi_file.txt" {
			t.Errorf("offered %v", got)
		}
		if letters := suggested(m, "/view @mfl"); len(letters) == 0 {
			t.Error("the letters of a name, in order, found nothing")
		}
	})

	t.Run("VS-02 /view with * ? or ** offers the files the pattern names", func(t *testing.T) {
		m := searchModel(t)

		deep := suggested(m, "/view **/*mi_file.*")
		some := suggested(m, "/view src/**/mi_file.*")
		single := suggested(m, "/view docs/*.md")

		if strings.Join(deep, ",") != "mi_file.go,docs/MI_FILE.md,src/pkg/mi_file.txt,src/deep/er/mi_file.test.go" {
			t.Errorf("**/*mi_file.* offered %v", deep)
		}
		if strings.Join(some, ",") != "src/pkg/mi_file.txt,src/deep/er/mi_file.test.go" || strings.Join(single, ",") != "docs/MI_FILE.md" {
			t.Errorf("src/**/mi_file.* offered %v, docs/*.md %v", some, single)
		}
	})

	t.Run("VS-03 a pattern with no / is matched on names at any depth; capitals do not matter", func(t *testing.T) {
		m := searchModel(t)

		goFiles := suggested(m, "/view *.go")
		md := suggested(m, "/view *.MD")

		if strings.Join(goFiles, ",") != "mi_file.go,src/deep/er/mi_file.test.go" || strings.Join(md, ",") != "docs/MI_FILE.md,src/pkg/readme.md" {
			t.Errorf("*.go offered %v, *.MD %v", goFiles, md)
		}
	})

	t.Run("VS-04 a word that is no path in the folder offers the files with it in their name", func(t *testing.T) {
		m := searchModel(t)

		got := suggested(m, "/view readme")
		inName := suggested(m, "/view file")

		if strings.Join(got, ",") != "src/pkg/readme.md" || len(inName) != 4 {
			t.Errorf("readme offered %v, file %v", got, inName)
		}
		// A start that a file of the folder has is still completed as a path.
		if prefix := suggested(m, "/view mi_"); strings.Join(prefix, ",") != "mi_file.go" {
			t.Errorf("mi_ offered %v", prefix)
		}
		// A path that exists keeps being completed as a path.
		if folder := suggested(m, "/view src/"); strings.Join(folder, ",") != "src/deep/,src/pkg/" {
			t.Errorf("src/ offered %v", folder)
		}
	})

	t.Run("VS-05 /view of a search shows the one file it finds, or lists the files to pick from", func(t *testing.T) {
		m := searchModel(t)

		one, _ := m.runCommand("/view @readme")
		many, _ := m.runCommand("/view **/*mi_file.*")
		byName, _ := m.runCommand("/view other")

		if viewer := one.(model).viewer; viewer == nil || viewer.file != "src/pkg/readme.md" {
			t.Errorf("one match: viewer %+v", viewer)
		}
		if many.(model).settings == nil || many.(model).viewer != nil {
			t.Error("several matches should be listed to pick from")
		}
		if viewer := byName.(model).viewer; viewer == nil || viewer.file != "other.txt" {
			t.Errorf("a name: viewer %+v", viewer)
		}
	})

	t.Run("VS-06 /view with no argument, or a folder, opens the tree of the folder", func(t *testing.T) {
		m := searchModel(t)

		none, _ := m.runCommand("/view")
		folder, _ := m.runCommand("/view src")

		for name, opened := range map[string]tea.Model{"no argument": none, "a folder": folder} {
			if viewer := opened.(model).viewer; viewer == nil || viewer.tree == nil || !viewer.placeholder || !viewer.treeFocus {
				t.Errorf("%s: viewer %+v", name, viewer)
			}
		}
		if root := folder.(model).viewer.tree.root; root != filepath.Join(m.session.WorkDir, "src") {
			t.Errorf("the tree is of %s", root)
		}
		narrow := m
		narrow.width = 50
		picker, _ := narrow.runCommand("/view")
		if picker.(model).settings == nil || picker.(model).viewer != nil {
			t.Error("a terminal too narrow for the tree lists the folder to pick from")
		}
	})

	t.Run("VS-07 hidden folders and dependencies are left out of searches", func(t *testing.T) {
		m := searchModel(t)

		got := strings.Join(suggested(m, "/view **/*mi_file*"), ",")

		for _, out := range []string{"node_modules", ".hidden", "vendor"} {
			if strings.Contains(got, out) {
				t.Errorf("%s was offered: %s", out, got)
			}
		}
	})

	t.Run("VS-08 browsing: .. goes up, a folder lists that, a file is viewed", func(t *testing.T) {
		m := searchModel(t)
		start, _ := m.browse(filepath.Join(m.session.WorkDir, "src", "pkg"))
		listed := ansi.Strip(start.(model).settings.form.View())

		if !strings.Contains(listed, "./src/pkg/") || !strings.Contains(listed, "mi_file.txt") || !strings.Contains(listed, "readme.md") || !strings.Contains(listed, "..") {
			t.Errorf("list:\n%s", listed)
		}
	})

	t.Run("VS-09 a search that finds nothing says so", func(t *testing.T) {
		m := searchModel(t)

		none, _ := m.runCommand("/view @zzzqqq")
		globNone, _ := m.runCommand("/view **/*.nothing")

		if !strings.Contains(shown(none.(model)), "no file matches @zzzqqq") || !strings.Contains(shown(globNone.(model)), "no file matches **/*.nothing") {
			t.Errorf("%s | %s", shown(none.(model)), shown(globNone.(model)))
		}
	})

	t.Run("VS-10 a big file opens at once and is coloured only where the viewer shows it", func(t *testing.T) {
		m := searchModel(t)
		var src strings.Builder
		for i := range 60000 {
			fmt.Fprintf(&src, "func f%d(a int) string { return fmt.Sprint(\"v\", a) } // %d\n", i, i)
		}
		_ = os.WriteFile(filepath.Join(m.session.WorkDir, "big.go"), []byte(src.String()), 0o644)

		start := time.Now()
		opened, _ := m.runCommand("/view big.go")
		view := opened.(model).View().Content
		took := time.Since(start)

		v := opened.(model).viewer
		if v == nil || v.code == nil || v.colored != nil || len(v.lines) != 60000 || !v.coloured() {
			t.Fatalf("viewer %+v", v)
		}
		if took > 2*time.Second || !strings.Contains(view, "\x1b[") {
			t.Errorf("took %v, coloured: %v", took, strings.Contains(view, "\x1b["))
		}
		if line := v.colouredLine(45000, true); !strings.Contains(line, keywordColor) {
			t.Errorf("a line far from the top: %q", line)
		}
	})

	t.Run("VS-11 the globs: ** across folders, * within one, ? a character", func(t *testing.T) {
		files := []string{"a.go", "src/a.go", "src/x/a.go", "src/x/y/b.go", "main.go", "ab.go"}
		cases := map[string]string{
			"**/a.go":    "a.go,src/a.go,src/x/a.go",
			"src/*.go":   "src/a.go",
			"src/**.go":  "src/a.go,src/x/a.go,src/x/y/b.go",
			"?.go":       "a.go,src/a.go,src/x/a.go,src/x/y/b.go",
			"*.go":       "a.go,ab.go,main.go,src/a.go,src/x/a.go,src/x/y/b.go",
			"src/x/?.go": "src/x/a.go",
		}
		for pattern, want := range cases {
			got := strings.Join(globFiles(files, pattern), ",")
			if got != want {
				t.Errorf("%s: %s, want %s", pattern, got, want)
			}
		}
	})

	t.Run("VS-12 navigating the list: a folder lists that, .. goes up, a file opens", func(t *testing.T) {
		m := searchModel(t)
		root := m.session.WorkDir
		listed := func(next tea.Model) string { return ansi.Strip(next.(model).settings.form.View()) }

		inSrc, _ := m.browseChoice(root, "src/")
		inPkg, _ := inSrc.(model).browseChoice(filepath.Join(root, "src"), "pkg/")
		up, _ := inPkg.(model).browseChoice(filepath.Join(root, "src", "pkg"), "..")
		file, _ := inPkg.(model).browseChoice(filepath.Join(root, "src", "pkg"), "readme.md")

		if !strings.Contains(listed(inSrc), "./src/ ·") || !strings.Contains(listed(inPkg), "./src/pkg/ ·") || !strings.Contains(listed(inPkg), "mi_file.txt") {
			t.Fatalf("src:\n%s\npkg:\n%s", listed(inSrc), listed(inPkg))
		}
		if !strings.Contains(listed(up), "./src/ ·") || !strings.Contains(listed(up), "▸ pkg/") {
			t.Errorf("up:\n%s", listed(up))
		}
		if viewer := file.(model).viewer; viewer == nil || viewer.file != filepath.Join(root, "src", "pkg", "readme.md") {
			t.Errorf("viewer %+v", viewer)
		}
	})

	t.Run("VS-13 a pattern being typed, as src/*/ or **/mi, offers the files whose paths it starts", func(t *testing.T) {
		m := searchModel(t)

		folder := suggested(m, "/view src/*/")
		inside := suggested(m, "/view s*/p*/r")
		anyDepth := suggested(m, "/view **/mi_file.t")
		everything := suggested(m, "/view **/")

		if strings.Join(folder, ",") != "src/pkg/mi_file.txt,src/pkg/readme.md" || strings.Join(inside, ",") != "src/pkg/readme.md" {
			t.Errorf("src/*/ offered %v, s*/p*/r %v", folder, inside)
		}
		if strings.Join(anyDepth, ",") != "src/pkg/mi_file.txt,src/deep/er/mi_file.test.go" || len(everything) != 6 {
			t.Errorf("**/mi_file.t offered %v, **/ %d files", anyDepth, len(everything))
		}
	})
}
