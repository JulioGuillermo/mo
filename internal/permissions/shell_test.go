package permissions

import (
	"strings"
	"testing"
)

func TestCommandSource(t *testing.T) {
	t.Run("each part keeps its source as written, quotes and redirections, beside its words", func(t *testing.T) {
		commands, err := Commands(`echo "a  b" | grep -v 'x' > f; ls $(cat "list file")`)
		if err != nil {
			t.Fatal(err)
		}
		var sources, texts []string
		for _, command := range commands {
			sources = append(sources, command.Source)
			texts = append(texts, command.Text)
		}
		want := []string{`echo "a  b"`, `grep -v 'x' > f`, `ls $(cat "list file")`, `cat "list file"`}
		if strings.Join(sources, "|") != strings.Join(want, "|") {
			t.Errorf("sources = %q", sources)
		}
		if texts[0] != "echo a  b" {
			t.Errorf("rules still see the words: %q", texts)
		}
	})
}
