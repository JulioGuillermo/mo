package skills

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | SK-01 | Happy    | A SKILL.md front matter gives name and description, quoted or as a block | P0       |
// | SK-02 | Happy    | A skill found in a later folder replaces the earlier one                 | P0       |
// | SK-03 | Edge     | Folders without SKILL.md and missing folders are skipped                 | P1       |
// | SK-04 | Edge     | Without front matter the folder names it and its first line describes it | P1      |
// | SK-05 | Happy    | Body leaves the front matter out; Files lists the other files            | P1       |

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, folder, content string) string {
	t.Helper()
	path := filepath.Join(dir, folder)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(path, FileName), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestFind(t *testing.T) {
	t.Run("SK-01 a SKILL.md front matter gives name and description, quoted or as a block", func(t *testing.T) {
		dir := t.TempDir()
		writeSkill(t, dir, "pdf", "---\nname: \"pdf\"\ndescription: >\n  Read and fill\n  PDF forms.\nlicense: MIT\n---\n# PDF\n")
		writeSkill(t, dir, "deploy", "---\nname: deploy\ndescription: 'Ship it: the steps'\n---\nbody")

		found := Find([]string{dir})

		if len(found) != 2 || found[0].Name != "deploy" || found[0].Description != "Ship it: the steps" || found[1].Description != "Read and fill PDF forms." {
			t.Errorf("found = %+v", found)
		}
	})

	t.Run("SK-02 a skill found in a later folder replaces the earlier one", func(t *testing.T) {
		user, project := t.TempDir(), t.TempDir()
		writeSkill(t, user, "review", "---\nname: review\ndescription: mine\n---\n")
		writeSkill(t, project, "review", "---\nname: review\ndescription: the team's\n---\n")

		found := Find([]string{user, project})

		if len(found) != 1 || found[0].Description != "the team's" {
			t.Errorf("found = %+v", found)
		}
	})

	t.Run("SK-03 folders without SKILL.md and missing folders are skipped", func(t *testing.T) {
		dir := t.TempDir()
		writeSkill(t, dir, "empty", "")
		os.WriteFile(filepath.Join(dir, "loose.md"), []byte("x"), 0o644)

		if found := Find([]string{dir, filepath.Join(dir, "nowhere")}); len(found) != 0 {
			t.Errorf("found = %+v", found)
		}
	})

	t.Run("SK-04 without front matter the folder names it and its first line describes it", func(t *testing.T) {
		dir := t.TempDir()
		writeSkill(t, dir, "notes", "\n# Taking notes the team's way\n\nmore")

		found := Find([]string{dir})

		if len(found) != 1 || found[0].Name != "notes" || found[0].Description != "Taking notes the team's way" {
			t.Errorf("found = %+v", found)
		}
	})

	t.Run("SK-05 Body leaves the front matter out; Files lists the other files", func(t *testing.T) {
		dir := t.TempDir()
		folder := writeSkill(t, dir, "pdf", "---\nname: pdf\n---\n\nStep one.\n")
		os.MkdirAll(filepath.Join(folder, "scripts"), 0o755)
		os.WriteFile(filepath.Join(folder, "scripts", "fill.py"), []byte("x"), 0o644)
		os.WriteFile(filepath.Join(folder, "forms.md"), []byte("x"), 0o644)
		skill, _ := Read(folder)

		body, err := skill.Body()
		files := skill.Files(10)

		if err != nil || body != "Step one." || !slices.Equal(files, []string{"forms.md", filepath.Join("scripts", "fill.py")}) || strings.Contains(body, "---") {
			t.Errorf("body %q files %v err %v", body, files, err)
		}
	})
}
