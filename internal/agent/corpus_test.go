package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/jg/mo/internal/llm"
)

// TestCorpusIsQuiet feeds the repository's own files, as text, to the stream watch: real files are not loops.
func TestCorpusIsQuiet(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the whole repository")
	}
	root := filepath.Join("..", "..")
	checked := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".md", ".toml", ".lua", ".json", ".txt":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 400_000 {
			return nil
		}
		checked++
		text := string(data)
		if found := feed(newRepeatWatch(5), llm.EventText, text); found != "" {
			t.Errorf("%s: %q", path, Quote(found))
		}
		return nil
	})
	if checked < 20 {
		t.Errorf("only %d files checked", checked)
	}
}

func feed(watch *repeatWatch, kind llm.EventKind, text string) string {
	for start := 0; start < len(text); start += 64 {
		if found := watch.add(llm.Event{Kind: kind, Call: 0, Text: text[start:min(start+64, len(text))]}); found != "" {
			return found
		}
	}
	return ""
}
