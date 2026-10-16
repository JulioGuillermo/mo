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

func TestShellScripts(t *testing.T) {
	texts := func(commands []Command) []string {
		var out []string
		for _, command := range commands {
			out = append(out, command.Text)
		}
		return out
	}

	t.Run("a written-out bash -c script is split into its own commands, the shell no part of its own", func(t *testing.T) {
		script := `bash -c '
cd /home/jg/kali-mcp
MCP_AUTH_TOKEN="smoke-secret" /tmp/agent-mcp-server >/tmp/agent-mcp.log 2>&1 &
PID=$!
SESS=$(curl -si -X POST localhost:2288/mcp | grep -i "^mcp-session-id:" | awk "{print \$(2)}")
echo "sessionid: $SESS"
kill $PID 2>/dev/null; wait $PID 2>/dev/null
'`
		commands, err := Commands(script)
		got := texts(commands)
		want := []string{"cd /home/jg/kali-mcp", "MCP_AUTH_TOKEN=smoke-secret /tmp/agent-mcp-server", "PID=$!", "SESS=$(…)",
			"curl -si -X POST localhost:2288/mcp", "grep -i ^mcp-session-id:", "awk {print $(2)}", `echo "sessionid: $SESS"`, "kill $PID", "wait $PID"}
		if err != nil || strings.Join(got, " | ") != strings.Join(want, " | ") {
			t.Errorf("got  %q\nwant %q (%v)", got, want, err)
		}
		for _, command := range commands {
			if strings.Contains(command.Source, "\n") || len(command.Source) > 80 {
				t.Errorf("a part should show short: %q", command.Source)
			}
		}
	})

	t.Run("a shell whose script is not written out, or that writes a file, stays a part of its own", func(t *testing.T) {
		for script, want := range map[string]string{
			`bash -c "$SCRIPT"`:      `bash -c "$SCRIPT"`,
			`sh -c 'ls' > out.txt`:   "sh -c ls",
			`PATH=/tmp/x bash -c ls`: "PATH=/tmp/x bash -c ls",
		} {
			commands, _ := Commands(script)
			if got := texts(commands); len(got) == 0 || got[0] != want {
				t.Errorf("%s: %q", script, got)
			}
		}
		lc, _ := Commands(`bash -lc 'make test && make lint'`)
		if got := texts(lc); strings.Join(got, "|") != "make test|make lint" {
			t.Errorf("bash -lc: %q", got)
		}
	})
}
