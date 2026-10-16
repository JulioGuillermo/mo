package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | RD-01 | Error    | A file over read_max_size is refused whole, saying its size and how to read a part | P0 |
// | RD-02 | Happy    | A part of a file over the limit reads as it would from a small one        | P0       |
// | RD-03 | Error    | Binary data without a NUL is told by its control characters; text with ANSI colours is not | P1 |
// | RD-04 | Happy    | A UTF-16 file reads as text                                               | P2       |
// | RD-05 | Happy    | An image over the limit is still seen                                     | P0       |
// | RD-06 | Edge     | A size is said as people read it                                          | P2       |

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/jg/mo/internal/llm"
)

func callLimited(t *testing.T, env Env, limit int64, fields map[string]any) (string, bool) {
	t.Helper()
	return NewRegistry(env, Options{MaxOutput: 10000, ReadMaxSize: limit}).Run(context.Background(), llm.ToolCall{Name: "read", Args: args(t, fields)})
}

func TestReadLimits(t *testing.T) {
	t.Run("RD-01 a file over read_max_size is refused whole, saying its size and how to read a part", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "big.log", strings.Repeat("a line of the log\n", 200))

		output, isError := callLimited(t, env, 1024, map[string]any{"path": "big.log"})

		if !isError || !strings.Contains(output, "3.5 KB") || !strings.Contains(output, "1 KB") || !strings.Contains(output, "offset") {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("RD-02 a part of a file over the limit reads as it would from a small one", func(t *testing.T) {
		env := newEnv(t)
		var text strings.Builder
		for i := range 300 {
			fmt.Fprintf(&text, "line %d %s\n", i, strings.Repeat("x", i%7*400))
		}
		for _, content := range []string{text.String(), strings.TrimSuffix(text.String(), "\n"), "only\n"} {
			writeFile(t, env, "big.txt", content)
			for _, view := range []map[string]any{
				{"limit": 3}, {"offset": 5, "limit": 4}, {"offset": -3}, {"offset": -1000}, {"offset": 400},
				{"match": "line 2[0-9]$"}, {"match": "line 1", "offset": -2, "limit": 1}, {"match": "nothing"},
				{"col": 10, "width": 30, "limit": 5, "offset": 1}, {"numbered": true, "offset": 298},
			} {
				fields := map[string]any{"path": "big.txt"}
				for key, value := range view {
					fields[key] = value
				}
				whole, _ := callLimited(t, env, 0, fields)
				part, isError := callLimited(t, env, 1, fields)
				if isError || part != whole {
					t.Errorf("view %v:\npart  %q\nwhole %q", view, shortened(part), shortened(whole))
				}
			}
		}
	})

	t.Run("RD-03 binary data without a NUL is told by its control characters; text with ANSI colours is not", func(t *testing.T) {
		env := newEnv(t)
		var data []byte
		for i := range 4000 {
			data = append(data, byte(1+i%250))
		}
		writeFile(t, env, "blob", string(data))
		writeFile(t, env, "colours.log", strings.Repeat("\x1b[31merror\x1b[m done\tok\r\n", 50))

		_, binaryErr := callLimited(t, env, 0, map[string]any{"path": "blob"})
		text, textErr := callLimited(t, env, 0, map[string]any{"path": "colours.log"})

		if !binaryErr {
			t.Error("blob read as text")
		}
		if textErr || !strings.Contains(text, "error") {
			t.Errorf("colours: %q", shortened(text))
		}
	})

	t.Run("RD-04 a UTF-16 file reads as text", func(t *testing.T) {
		env := newEnv(t)
		units := utf16.Encode([]rune("año\r\nniño\r\n"))
		data := []byte{0xff, 0xfe}
		for _, unit := range units {
			data = binary.LittleEndian.AppendUint16(data, unit)
		}
		writeFile(t, env, "win.txt", string(data))

		output, isError := callLimited(t, env, 0, map[string]any{"path": "win.txt"})

		if isError || output != "año\nniño\n" {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("RD-05 an image over the limit is still seen", func(t *testing.T) {
		env := newEnv(t)
		file, err := os.Create(filepath.Join(env.WorkDir, "pic.png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(file, image.NewRGBA(image.Rect(0, 0, 300, 200)))
		file.Close()

		output, isError := callLimited(t, env, 1, map[string]any{"path": "pic.png"})

		if isError || !strings.Contains(output, "png 300x200") {
			t.Errorf("output = %q", output)
		}
	})

	t.Run("RD-06 a size is said as people read it", func(t *testing.T) {
		for size, want := range map[int64]string{3435973837: "3.2 GB", 10 << 20: "10 MB", 1536: "1.5 KB", 12: "12 bytes"} {
			if got := formatSize(size); got != want {
				t.Errorf("formatSize(%d) = %q", size, got)
			}
		}
	})
}

func shortened(text string) string {
	if len(text) > 300 {
		return text[:300] + "…"
	}
	return text
}
