package sessions

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SS-01 | Happy    | A saved conversation loads back whole, images included               | P0       |
// | SS-02 | Happy    | Only the folder's conversations are listed, the latest first         | P0       |
// | SS-03 | Security | Files are the user's only; an id cannot leave the folder             | P0       |
// | SS-04 | Edge     | The title is the first line of the first prompt, cut short           | P2       |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
)

func TestSessions(t *testing.T) {
	t.Run("SS-01 a saved conversation loads back whole, images included", func(t *testing.T) {
		dir := t.TempDir()
		record := Record{Info: Info{ID: NewID(), WorkDir: "/w", Title: "hi", Usage: llm.Usage{InputTokens: 9}}, History: []llm.Message{
			{Role: llm.RoleUser, Text: "hi", Images: []llm.Image{{MIME: "image/png", Data: []byte{1, 2, 3}}}},
			{Role: llm.RoleAssistant, Text: "hello", Reasoning: "think", ToolCalls: []llm.ToolCall{{ID: "a", Name: "ls", Args: []byte(`{}`)}}},
		}}

		err := Save(dir, record)
		loaded, loadErr := Load(dir, record.ID)

		if err != nil || loadErr != nil || loaded.History[0].Images[0].Data[2] != 3 || loaded.History[1].Reasoning != "think" || string(loaded.History[1].ToolCalls[0].Args) != "{}" || loaded.Usage.InputTokens != 9 {
			t.Errorf("loaded = %+v err = %v %v", loaded, err, loadErr)
		}
	})

	t.Run("SS-02 only the folder's conversations are listed, the latest first", func(t *testing.T) {
		dir := t.TempDir()
		now := time.Now()
		_ = Save(dir, Record{Info: Info{ID: "a", WorkDir: "/w", Updated: now.Add(-time.Hour)}})
		_ = Save(dir, Record{Info: Info{ID: "b", WorkDir: "/w", Updated: now}})
		_ = Save(dir, Record{Info: Info{ID: "c", WorkDir: "/other", Updated: now}})
		_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o600)

		infos, err := List(dir, "/w")
		missing, _ := List(filepath.Join(dir, "none"), "/w")

		if err != nil || len(infos) != 2 || infos[0].ID != "b" || infos[1].ID != "a" || missing != nil {
			t.Errorf("infos = %+v err = %v", infos, err)
		}
	})

	t.Run("SS-03 files are the user's only; an id cannot leave the folder", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "sessions")
		_ = Save(dir, Record{Info: Info{ID: "x"}})
		info, _ := os.Stat(filepath.Join(dir, "x.json"))
		folder, _ := os.Stat(dir)

		if info.Mode().Perm() != 0o600 || folder.Mode().Perm() != 0o700 {
			t.Errorf("file = %v folder = %v", info.Mode(), folder.Mode())
		}
		if Save(dir, Record{Info: Info{ID: "../evil"}}) == nil {
			t.Error("an id with a path was saved")
		}
		if _, err := Load(dir, "../x"); err == nil {
			t.Error("an id with a path was loaded")
		}
	})

	t.Run("SS-04 the title is the first line of the first prompt, cut short", func(t *testing.T) {
		long := Title(strings.Repeat("a", 200))

		if Title("  fix the bug\nin main.go") != "fix the bug" || len([]rune(long)) != titleLength {
			t.Errorf("titles = %q %q", Title("fix the bug\nx"), long)
		}
	})
}
