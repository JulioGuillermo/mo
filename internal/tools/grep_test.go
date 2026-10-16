package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | GR-01 | Happy    | A file of any size is searched, to its last line                         | P0       |
// | GR-02 | Edge     | Close matches share their context, each line shown once, and the last match's context follows the cut | P1 |
// | GR-03 | Edge     | Binary data is skipped by its start, NUL or not                           | P1       |
// | GR-04 | Happy    | A search cancelled stops and says so                                      | P1       |

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestGrepStreams(t *testing.T) {
	ctx := context.Background()

	t.Run("GR-01 a file of any size is searched, to its last line", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "big.log", strings.Repeat("nothing to see here\n", 200000)+"the needle\n")

		output, _ := runGrep(ctx, env, args(t, map[string]any{"pattern": "needle"}))
		counted, _ := runGrep(ctx, env, args(t, map[string]any{"pattern": "nothing", "show": "count"}))

		if output != "big.log\n200001: the needle\n" || counted != "big.log: 200000\n" {
			t.Errorf("output = %q counted = %q", output, counted)
		}
	})

	t.Run("GR-02 close matches share their context, each line shown once, and the last match's context follows the cut", func(t *testing.T) {
		env := newEnv(t)
		path := writeFile(t, env, "a.txt", "1\nx\n3\nx\n5\n6\n7\n8\nx\n10\n")
		var shared, cut strings.Builder

		grepFile(ctx, path, "a.txt", grepOptions{re: regexp.MustCompile("x"), output: "lines", context: 1}, &shared, 100)
		grepFile(ctx, path, "a.txt", grepOptions{re: regexp.MustCompile("x"), output: "lines", context: 1}, &cut, 1)

		if shared.String() != "a.txt\n1- 1\n2: x\n3- 3\n4: x\n5- 5\n--\n8- 8\n9: x\n10- 10\n" {
			t.Errorf("shared = %q", shared.String())
		}
		if cut.String() != "a.txt\n1- 1\n2: x\n3- 3\n" {
			t.Errorf("cut = %q", cut.String())
		}
	})

	t.Run("GR-03 binary data is skipped by its start, NUL or not", func(t *testing.T) {
		env := newEnv(t)
		var blob []byte
		for i := range 4000 {
			blob = append(blob, byte(1+i%250))
		}
		writeFile(t, env, "blob", string(blob)+"needle\n")
		writeFile(t, env, "nul", "needle\x00\n")
		writeFile(t, env, "text", "needle\n")

		output, _ := runGrep(ctx, env, args(t, map[string]any{"pattern": "needle", "show": "files"}))

		if output != "text\n" {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("GR-04 a search cancelled stops and says so", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "big.log", strings.Repeat("line\n", 100000))
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := runGrep(cancelled, env, args(t, map[string]any{"pattern": "zzz"}))

		if err == nil {
			t.Error("no error from a cancelled search")
		}
	})
}
