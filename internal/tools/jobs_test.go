package tools

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | JB-01 | Happy    | A background command returns at once; its end is told once, with its output | P0 |
// | JB-02 | Happy    | Killing a job stops what it started and is not told as an end        | P0       |
// | JB-03 | Security | Closing kills every job left, and nothing starts after              | P0       |
// | JB-04 | Error    | Without jobs, background is refused instead of run in the foreground  | P1       |
// | JB-05 | Happy    | A foreground command is a job: its whole output stays to read by lines | P0      |
// | JB-06 | Happy    | Long output comes back cut, with the job to read the rest             | P0       |
// | JB-07 | Happy    | match searches one job or every job of the conversation             | P1       |
// | JB-11 | Happy    | The user sends a blocking command to the background; the model hears when it ends | P0 |
// | JB-13 | Happy    | In memory the output reads as on disk, and nothing reaches the folder | P0 |
// | JB-14 | Happy    | With none only the start and end come back, and output says it was not kept | P1 |
// | JB-15 | Happy    | Output older than the turns kept goes; that of later turns stays      | P0 |
// | JB-16 | Edge     | Switching conversations prunes nothing until a turn of the new one starts | P0 |
// | JB-17 | Happy    | A server in the background returns once a line matches until, going on | P0 |
// | JB-18 | Edge     | One that dies before returns at once with its exit, not told again later | P0 |
// | JB-12 | Happy    | Reading output and help never ask; running and killing do          | P0       |
// | JB-08 | Edge     | A log keeps its first and last bytes and says how much it dropped    | P0       |
// | JB-09 | Edge     | Each conversation numbers and sees its own jobs, also after a restart | P0      |
// | JB-10 | Edge     | Past the conversation's limit the oldest output goes                 | P1       |

import (
	"context"
	"fmt"
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
	jobs := NewJobs(t.TempDir())
	jobs.SetConversation("c1")
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

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"action": "kill", "id": 1})})
		time.Sleep(1300 * time.Millisecond)

		_, err := os.Stat(marker)
		if isError || output != "job 1 killed" || jobs.List()[0].State != JobKilled || len(jobs.TakeEnded()) != 0 || err == nil {
			t.Errorf("output = %q state = %s child ran = %v", output, jobs.List()[0].State, err == nil)
		}
	})

	t.Run("JB-03 closing kills every job left, and nothing starts after", func(t *testing.T) {
		jobs := NewJobs(t.TempDir())
		jobs.SetConversation("c1")
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

	t.Run("JB-05 a foreground command is a job: its whole output stays to read by lines", func(t *testing.T) {
		registry, _ := jobsRegistry(t)

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "seq 1 5; exit 2"})})
		read, readError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"action": "output", "id": 1, "offset": 1, "limit": 2, "numbered": true})})
		list, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"action": "output"})})

		if !isError || !strings.Contains(output, "5\n[job 1]") || !strings.Contains(output, "exit status 2") {
			t.Errorf("output = %q", output)
		}
		if readError || !strings.HasSuffix(read, "\n2\t2\n3\t3\n[lines 2-3 of 5; offset=3 continues]\n") {
			t.Errorf("read = %q", read)
		}
		if !strings.Contains(list, "job 1 failed with exit 2") {
			t.Errorf("list = %q", list)
		}
	})

	t.Run("JB-06 long output comes back cut, with the job to read the rest", func(t *testing.T) {
		jobs := NewJobs(t.TempDir())
		jobs.SetConversation("c1")
		t.Cleanup(jobs.Close)
		registry := NewRegistry(Env{WorkDir: t.TempDir(), Jobs: jobs}, Options{MaxOutput: 500})

		output, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "seq 1 5000"})})
		middle, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"id": 1, "offset": 2499, "limit": 1})})

		if len(output) > 500 || !strings.Contains(output, "action output with id 1") || !strings.HasPrefix(output, "1\n2\n") {
			t.Errorf("output (%d bytes) = %q", len(output), output)
		}
		if !strings.Contains(middle, "\n2500\n[lines 2500-2500 of 5000") {
			t.Errorf("middle = %q", middle)
		}
	})

	t.Run("JB-07 match searches one job or every job of the conversation", func(t *testing.T) {
		registry, _ := jobsRegistry(t)
		registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "echo ok; echo error: one"})})
		registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "echo ERROR: two"})})

		every, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"action": "output", "match": "(?i)error"})})
		one, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"action": "output", "id": 2, "match": "error"})})

		if every != "1:2: error: one\n2:1: ERROR: two\n" {
			t.Errorf("every = %q", every)
		}
		if !strings.HasSuffix(one, "no line matches \"error\"") {
			t.Errorf("one = %q", one)
		}
	})

	t.Run("JB-08 a log keeps its first and last bytes and says how much it dropped", func(t *testing.T) {
		head, tail := jobLogHead, jobLogTail
		jobLogHead, jobLogTail = 10, 10
		t.Cleanup(func() { jobLogHead, jobLogTail = head, tail })
		path := filepath.Join(t.TempDir(), "1.log")
		log, err := newJobLog(path)
		if err != nil {
			t.Fatal(err)
		}

		for i := range 20 {
			fmt.Fprintf(log, "line %02d\n", i)
		}
		running, _ := log.Text()
		log.Close()
		ended, _ := os.ReadFile(path)

		if !strings.HasPrefix(string(ended), "line 00\nli") || !strings.HasSuffix(string(ended), "line 19\n") || !strings.Contains(string(ended), "bytes cut") {
			t.Errorf("ended = %q", ended)
		}
		if !strings.HasSuffix(running, "line 19\n") || log.Size() != 160 {
			t.Errorf("running = %q size = %d", running, log.Size())
		}
		if _, err := os.Stat(path + ".tail"); !os.IsNotExist(err) {
			t.Error("the tail file was left behind")
		}
	})

	t.Run("JB-09 each conversation numbers and sees its own jobs, also after a restart", func(t *testing.T) {
		root := t.TempDir()
		env := Env{WorkDir: t.TempDir()}
		first := NewJobs(root)
		first.SetConversation("a")
		first.Run(context.Background(), env, "echo from a", time.Minute)
		first.SetConversation("b")
		first.Run(context.Background(), env, "echo from b", time.Minute)
		first.Close()

		again := NewJobs(root)
		t.Cleanup(again.Close)
		again.SetConversation("a")
		status, _, _ := again.Run(context.Background(), env, "echo again", time.Minute)
		_, text, err := again.Text(1)

		if status.ID != 2 || err != nil || text != "from a\n" || len(again.All()) != 2 {
			t.Errorf("id = %d text = %q err = %v all = %+v", status.ID, text, err, again.All())
		}
	})

	t.Run("JB-10 past the conversation's limit the oldest output goes", func(t *testing.T) {
		kept := jobsKeptBytes
		jobsKeptBytes = 15
		t.Cleanup(func() { jobsKeptBytes = kept })
		jobs := NewJobs(t.TempDir())
		jobs.SetConversation("c1")
		t.Cleanup(jobs.Close)
		env := Env{WorkDir: t.TempDir(), Jobs: jobs}

		jobs.Run(context.Background(), env, "echo first output", time.Minute)
		jobs.Run(context.Background(), env, "echo second output", time.Minute)
		jobs.Run(context.Background(), env, "echo third", time.Minute)
		_, _, firstErr := jobs.Text(1)
		_, second, secondErr := jobs.Text(2)

		if firstErr == nil || !strings.Contains(firstErr.Error(), "deleted") || secondErr != nil || second != "second output\n" {
			t.Errorf("first err = %v second = %q err = %v", firstErr, second, secondErr)
		}
	})

	t.Run("JB-11 the user sends a blocking command to the background; the model hears when it ends", func(t *testing.T) {
		registry, jobs := jobsRegistry(t)
		told := make(chan JobStatus, 1)
		jobs.OnEnd(func(status JobStatus) { told <- status })
		go func() {
			waitFor(t, func() bool { return len(jobs.Detach()) == 1 })
		}()

		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "echo started; sleep 0.5; echo finished"})})
		ended := <-told

		if isError || !strings.Contains(output, "sent this command to the background") || !strings.Contains(output, "[job 1]") {
			t.Errorf("output = %q", output)
		}
		if _, text, _ := jobs.Text(1); ended.State != JobDone || text != "started\nfinished\n" || jobs.List()[0].Foreground {
			t.Errorf("ended = %+v text = %q", ended, text)
		}
	})

	t.Run("JB-12 reading output and help never ask; running and killing do", func(t *testing.T) {
		registry, _ := jobsRegistry(t)
		for fields, changes := range map[string]bool{
			`{"command":"ls"}`: true, `{"action":"kill","id":1}`: true,
			`{"action":"output","id":1}`: false, `{"id":1}`: false, `{"action":"help"}`: false,
		} {
			call := llm.ToolCall{Name: "bash", Args: []byte(fields)}
			if registry.Changes(call) != changes || registry.AlreadyConfirmed(call) == changes {
				t.Errorf("%s: changes = %v", fields, registry.Changes(call))
			}
		}
	})

	t.Run("JB-13 in memory the output reads as on disk, and nothing reaches the folder", func(t *testing.T) {
		root := t.TempDir()
		jobs := NewJobs(root)
		jobs.SetConversation("c1")
		jobs.Configure(OutputsMemory, 0)
		t.Cleanup(jobs.Close)
		registry := NewRegistry(Env{WorkDir: t.TempDir(), Jobs: jobs}, Options{MaxOutput: 10000})

		registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "seq 1 3"})})
		read, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"id": 1, "offset": 1, "limit": 1})})
		list, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"action": "output"})})
		entries, _ := os.ReadDir(root)

		if !strings.Contains(read, "\n2\n[lines 2-2 of 3") || !strings.Contains(list, "job 1 done") || len(entries) != 0 {
			t.Errorf("read = %q list = %q folder = %d entries", read, list, len(entries))
		}
	})

	t.Run("JB-14 with none only the start and end come back, and output says it was not kept", func(t *testing.T) {
		jobs := NewJobs(t.TempDir())
		jobs.SetConversation("c1")
		jobs.Configure(OutputsNone, 0)
		t.Cleanup(jobs.Close)
		registry := NewRegistry(Env{WorkDir: t.TempDir(), Jobs: jobs}, Options{MaxOutput: 300})

		ran, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"command": "seq 1 2000"})})
		read, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{"id": 1})})

		if !strings.Contains(ran, "the rest was not kept") || !isError || !strings.Contains(read, "not kept") {
			t.Errorf("ran = %q read = %q", ran, read)
		}
	})

	t.Run("JB-15 output older than the turns kept goes; that of later turns stays", func(t *testing.T) {
		jobs := NewJobs(t.TempDir())
		jobs.SetConversation("c1")
		jobs.Configure(OutputsDisk, 2)
		t.Cleanup(jobs.Close)
		env := Env{WorkDir: t.TempDir(), Jobs: jobs}
		for turn := 1; turn <= 3; turn++ {
			jobs.SetTurn(turn)
			jobs.Run(context.Background(), env, fmt.Sprintf("echo turn %d", turn), time.Minute)
		}
		jobs.SetTurn(4)

		_, _, first := jobs.Text(1)
		_, second, secondErr := jobs.Text(2)
		_, third, _ := jobs.Text(3)

		if first == nil || secondErr == nil {
			t.Errorf("turns 1 and 2 should be gone: %v %q", first, second)
		}
		if third != "turn 3\n" {
			t.Errorf("turn 3 should stay: %q", third)
		}
	})

	t.Run("JB-16 switching conversations prunes nothing until a turn of the new one starts", func(t *testing.T) {
		jobs := NewJobs(t.TempDir())
		t.Cleanup(jobs.Close)
		env := Env{WorkDir: t.TempDir(), Jobs: jobs}
		jobs.SetConversation("short")
		jobs.SetTurn(2)
		jobs.Run(context.Background(), env, "echo kept", time.Minute)
		jobs.SetConversation("long")
		jobs.SetTurn(40)
		jobs.SetConversation("short")

		jobs.Configure(OutputsDisk, 3)
		_, text, err := jobs.Text(1)

		if err != nil || text != "kept\n" {
			t.Errorf("text = %q err = %v", text, err)
		}
	})

	t.Run("JB-17 a server in the background returns once a line matches until, going on", func(t *testing.T) {
		registry, jobs := jobsRegistry(t)

		started := time.Now()
		output, isError := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{
			"command": "echo booting; sleep 0.3; echo listening on :8080; sleep 30", "background": true, "until": "listening on",
		})})

		if isError || !strings.Contains(output, `a line matched "listening on"`) || !strings.Contains(output, "booting\nlistening on :8080") || time.Since(started) > 5*time.Second {
			t.Errorf("output = %q", output)
		}
		if jobs.List()[0].State != JobRunning {
			t.Error("the server should go on")
		}
	})

	t.Run("JB-18 one that dies before returns at once with its exit, not told again later", func(t *testing.T) {
		registry, jobs := jobsRegistry(t)

		output, _ := registry.Run(context.Background(), llm.ToolCall{Name: "bash", Args: args(t, map[string]any{
			"command": "echo port in use; exit 3", "background": true, "until": "listening", "wait": 20,
		})})
		time.Sleep(200 * time.Millisecond)

		if !strings.Contains(output, "failed with exit 3") || !strings.Contains(output, "port in use") || len(jobs.TakeEnded()) != 0 {
			t.Errorf("output = %q", output)
		}
	})
}
