package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GR-10 | Happy    | A file they renamed takes the change we made at its old path          | P0       |
// | GR-11 | Happy    | A file we renamed takes the change they made at its old path          | P0       |
// | GR-12 | Happy    | A rename with edits still pairs when the file is mostly the same      | P1       |
// | GR-13 | Edge     | Renamed apart on each side, the content merges and the clash is reported | P2    |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const original = "package main\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() {}\n"

func (tr *testRepo) rename(from, to, content, message string) {
	tr.t.Helper()
	if _, err := tr.tree.Remove(from); err != nil {
		tr.t.Fatal(err)
	}
	tr.commit(message, map[string]string{to: content})
}

func exists(tr *testRepo, path string) bool {
	_, err := os.Stat(filepath.Join(tr.dir, path))
	return err == nil
}

func TestRenames(t *testing.T) {
	t.Run("GR-10 a file they renamed takes the change we made at its old path", func(t *testing.T) {
		tr := newRepo(t)
		tr.branch("feature", true)
		tr.rename("main.go", "app.go", original, "rename")
		tr.branch("master", false)
		tr.commit("edit a", map[string]string{"main.go": strings.Replace(original, "func a() {}", "func a() { edited() }", 1)})

		result, err := tr.r.Merge("feature")

		if err != nil || !strings.Contains(result, "merge committed") || exists(tr, "main.go") || !strings.Contains(tr.read("app.go"), "edited()") {
			t.Errorf("result = %q err = %v main.go left = %v app.go = %q", result, err, exists(tr, "main.go"), tr.read("app.go"))
		}
	})

	t.Run("GR-11 a file we renamed takes the change they made at its old path", func(t *testing.T) {
		tr := newRepo(t)
		tr.branch("feature", true)
		tr.commit("edit c", map[string]string{"main.go": strings.Replace(original, "func c() {}", "func c() { theirs() }", 1)})
		tr.branch("master", false)
		tr.rename("main.go", "app.go", original, "rename")

		result, err := tr.r.Merge("feature")

		if err != nil || !strings.Contains(result, "merge committed") || exists(tr, "main.go") || !strings.Contains(tr.read("app.go"), "theirs()") {
			t.Errorf("result = %q err = %v main.go left = %v app.go = %q", result, err, exists(tr, "main.go"), tr.read("app.go"))
		}
	})

	t.Run("GR-12 a rename with edits still pairs when the file is mostly the same", func(t *testing.T) {
		tr := newRepo(t)
		tr.branch("feature", true)
		tr.rename("main.go", "app.go", strings.Replace(original, "func b() {}", "func b() { moved() }", 1), "rename and edit")
		tr.branch("master", false)
		tr.commit("edit a", map[string]string{"main.go": strings.Replace(original, "func a() {}", "func a() { edited() }", 1)})

		_, err := tr.r.Merge("feature")

		app := tr.read("app.go")
		if err != nil || exists(tr, "main.go") || !strings.Contains(app, "edited()") || !strings.Contains(app, "moved()") {
			t.Errorf("err = %v main.go left = %v app.go =\n%s", err, exists(tr, "main.go"), app)
		}
	})

	t.Run("GR-13 renamed apart on each side, the content merges and the clash is reported", func(t *testing.T) {
		tr := newRepo(t)
		tr.branch("feature", true)
		tr.rename("main.go", "theirs.go", original, "rename there")
		tr.branch("master", false)
		tr.rename("main.go", "ours.go", original, "rename here")

		result, err := tr.r.Merge("feature")

		if err != nil || !strings.Contains(result, "renamed to ours.go here and to theirs.go there") || !exists(tr, "ours.go") {
			t.Errorf("result = %q err = %v", result, err)
		}
	})
}
