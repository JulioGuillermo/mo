//go:build !windows

package sysopen

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | SO-01 | Happy    | Open hands the target to the system's opener and does not wait      | P1       |
// | SO-02 | Error    | Without an opener, Open says so                                     | P2       |

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeOpener(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	got := filepath.Join(t.TempDir(), "opened")
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	script := "#!/bin/sh\nsleep 1\nprintf '%s' \"$1\" > " + got + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return got
}

func TestOpen(t *testing.T) {
	t.Run("SO-01 Open hands the target to the system's opener and does not wait", func(t *testing.T) {
		got := fakeOpener(t)
		start := time.Now()

		err := Open("https://go.dev/doc")

		took := time.Since(start)
		deadline := time.Now().Add(5 * time.Second)
		var opened []byte
		for time.Now().Before(deadline) {
			if opened, _ = os.ReadFile(got); len(opened) > 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil || took > 500*time.Millisecond || string(opened) != "https://go.dev/doc" {
			t.Errorf("err %v took %v opened %q", err, took, opened)
		}
	})

	t.Run("SO-02 without an opener, Open says so", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		err := Open("x")

		if err == nil || !strings.Contains(err.Error(), "executable file not found") {
			t.Errorf("err = %v", err)
		}
	})
}
