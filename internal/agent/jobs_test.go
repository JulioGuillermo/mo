package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | AJ-01 | Happy    | A job that ends during a turn is told at the model's next step       | P0       |
// | AJ-02 | Happy    | A job that ends while idle starts a turn from its note; none, no turn | P0      |

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

func jobsAgent(t *testing.T, provider llm.Provider) (*Agent, *tools.Jobs) {
	t.Helper()
	jobs := tools.NewJobs()
	t.Cleanup(jobs.Close)
	agent := newAgent(t, provider)
	agent.Tools = tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Jobs: jobs}, tools.Options{MaxOutput: 1000})
	return agent, jobs
}

func endedJob(t *testing.T, jobs *tools.Jobs, command string) {
	t.Helper()
	ended := make(chan struct{})
	jobs.OnEnd(func(tools.JobStatus) { close(ended) })
	if _, err := jobs.Start(tools.Env{WorkDir: t.TempDir()}, command); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not end")
	}
}

func TestBackgroundJobs(t *testing.T) {
	t.Run("AJ-01 a job that ends during a turn is told at the model's next step", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("w", "bash", `{"command":"true"}`)}},
			{Role: llm.RoleAssistant, Text: "seen"},
		}}
		agent, jobs := jobsAgent(t, provider)
		endedJob(t, jobs, "echo tests passed")

		_ = agent.Run(context.Background(), "go", Hooks{})

		first := provider.requests[0].Messages
		if text := first[len(first)-1].Text; !strings.HasPrefix(text, "go\n\n"+moNotePrefix+"Background job 1 done") || !strings.Contains(text, "tests passed") {
			t.Errorf("first request ends with %q", text)
		}
	})

	t.Run("AJ-02 a job that ends while idle starts a turn from its note; none, no turn", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "the server stopped"}}}
		agent, jobs := jobsAgent(t, provider)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "start the server"}, {Role: llm.RoleAssistant, Text: "started"}}

		quiet := agent.RunEndedJobs(context.Background(), Hooks{})
		requestsBefore := len(provider.requests)
		endedJob(t, jobs, "exit 1")
		err := agent.RunEndedJobs(context.Background(), Hooks{})

		if quiet != nil || requestsBefore != 0 || err != nil || len(provider.requests) != 1 {
			t.Fatalf("requests = %d err = %v", len(provider.requests), err)
		}
		note := agent.History[2]
		if note.Role != llm.RoleUser || !strings.Contains(note.Text, "failed with exit 1") || IsPrompt(note) {
			t.Errorf("note = %+v", note)
		}
	})
}
