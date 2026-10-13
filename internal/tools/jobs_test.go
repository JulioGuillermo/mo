package tools

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | JB-01 | Happy    | A background command returns at once; its end is told once, with its output | P0 |
// | JB-02 | Happy    | Killing a job stops what it started and is not told as an end        | P0       |
// | JB-03 | Security | Closing kills every job left, and nothing starts after              | P0       |
// | JB-04 | Error    | Without jobs, background is refused instead of run in the foreground  | P1       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
)

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func jobsRegistry(t *testing.T) (*Registry, *Jobs) {
	t.Helper()
	jobs := NewJobs()
	t.Cleanup(jobs.Close)
	return NewRegistry(Env{WorkDir: t.TempDir(), Jobs: jobs}, Options{MaxOutput: 10000}), jobs
}

func TestJobs(t *testing.T) {
	t.Run("JB-01 a background command returns at once; its end is told once, with its output", func(t *testing.T) {
		registry, jobs := jobsRegistry(t)
		told := make(chan JobStatus, 1)
		jobs.OnEnd(func(status JobStatus) { told <- status })

		started := time.Now()
		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "sleep 0.3; echo built; exit 3", "background": true})})
		took := time.Since(started)
		ended := <-told

		if isError || !strings.Contains(output, "started job 1") || took > 200*time.Millisecond {
			t.Fatalf("output = %q took = %v", output, took)
		}
		taken := jobs.TakeEnded()
		if ended.State != JobFailed || ended.ExitCode != 3 || len(taken) != 1 || len(jobs.TakeEnded()) != 0 {
			t.Errorf("ended = %+v taken = %+v", ended, taken)
		}
		if note := jobs.EndedNote(ended); !strings.Contains(note, "failed with exit 3") || !strings.Contains(note, "built") {
			t.Errorf("note = %q", note)
		}
	})

	t.Run("JB-02 killing a job stops what it started and is not told as an end", func(t *testing.T) {
		registry, jobs := jobsRegistry(t)
		marker := filepath.Join(t.TempDir(), "still-running")
		registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "(sleep 1; touch " + marker + ") & sleep 30", "background": true})})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "jobs", Args: args(t, map[string]any{"action": "kill", "id": 1})})
		time.Sleep(1300 * time.Millisecond)

		_, err := os.Stat(marker)
		if isError || output != "job 1 killed" || jobs.List()[0].State != JobKilled || len(jobs.TakeEnded()) != 0 || err == nil {
			t.Errorf("output = %q state = %s child ran = %v", output, jobs.List()[0].State, err == nil)
		}
	})

	t.Run("JB-03 closing kills every job left, and nothing starts after", func(t *testing.T) {
		jobs := NewJobs()
		env := Env{WorkDir: t.TempDir(), Jobs: jobs}
		_, _ = jobs.Start(env, "sleep 30")
		_, _ = jobs.Start(env, "sleep 30")

		jobs.Close()
		_, err := jobs.Start(env, "true")

		for _, status := range jobs.List() {
			if status.State != JobKilled {
				t.Errorf("job %d is %s", status.ID, status.State)
			}
		}
		if err == nil {
			t.Error("a job started after closing")
		}
	})

	t.Run("JB-04 without jobs, background is refused instead of run in the foreground", func(t *testing.T) {
		registry := NewRegistry(Env{WorkDir: t.TempDir()}, Options{MaxOutput: 1000})

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "sleep 5", "background": true})})

		if !isError || !strings.Contains(output, "not available") {
			t.Errorf("output = %q", output)
		}
	})
}
