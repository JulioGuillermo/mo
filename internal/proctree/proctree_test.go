//go:build !windows

package proctree

// Test Traceability Table
// | ID    | Category | Description                                                     | Priority |
// | PT-01 | Happy    | Kill ends the command and the children it started               | P0       |
// | PT-02 | Happy    | Terminate ends a tree that does not ignore it; Alive tells so    | P1       |
// | PT-03 | Happy    | KillOnCancel kills the tree when the context is cancelled       | P0       |
// | PT-04 | Security | A detached command has no terminal to prompt on                 | P1       |

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const waitToEnd = 3 * time.Second

func startTree(t *testing.T, ctx context.Context) (cmd *exec.Cmd, child int) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "child.pid")
	cmd = exec.CommandContext(ctx, "sh", "-c", "sleep 300 & echo $! > "+marker+"; wait")
	Detach(cmd)
	KillOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(waitToEnd)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(marker)
		if child, _ = strconv.Atoi(strings.TrimSpace(string(data))); child != 0 {
			return cmd, child
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the child never started")
	return nil, 0
}

func ended(pid int) bool {
	deadline := time.Now().Add(waitToEnd)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestProcTree(t *testing.T) {
	t.Run("PT-01 Kill ends the command and the children it started", func(t *testing.T) {
		cmd, child := startTree(t, context.Background())

		err := Kill(cmd.Process.Pid)
		_ = cmd.Wait()

		if err != nil || !ended(child) {
			t.Errorf("err = %v, child %d still running", err, child)
		}
	})

	t.Run("PT-02 Terminate ends a tree that does not ignore it; Alive tells so", func(t *testing.T) {
		cmd, child := startTree(t, context.Background())
		aliveBefore := Alive(cmd.Process.Pid)

		err := Terminate(cmd.Process.Pid)
		_ = cmd.Wait()

		if !aliveBefore || err != nil || !ended(child) || Alive(cmd.Process.Pid) {
			t.Errorf("alive before = %v err = %v, child %d", aliveBefore, err, child)
		}
	})

	t.Run("PT-03 KillOnCancel kills the tree when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd, child := startTree(t, ctx)

		cancel()
		_ = cmd.Wait()

		if !ended(child) {
			t.Errorf("child %d still running", child)
		}
	})

	t.Run("PT-04 A detached command has no terminal to prompt on", func(t *testing.T) {
		cmd := exec.Command("sh", "-c", "cat /dev/tty")
		Detach(cmd)

		err := cmd.Run()

		if err == nil {
			t.Error("reading the terminal worked")
		}
	})
}
