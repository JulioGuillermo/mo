package agent

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | LG-01 | Happy    | The third same call with the same result is not run and says why     | P0       |
// | LG-02 | Edge     | The same call with a new result each time keeps running              | P0       |
// | LG-03 | Edge     | Arguments in another key order are the same call                     | P1       |
// | LG-04 | Edge     | Calls older than the last replies window do not count                | P1       |
// | LG-05 | Edge     | Calls made before the user's last prompt do not count                | P0       |
// | LG-06 | Happy    | Past the warnings the turn stops, every call still gets a result     | P0       |
// | LG-07 | Happy    | A reply that repeats itself is cut, left out and the model is told   | P0       |
// | LG-08 | Happy    | A stream loop past the warnings stops the turn without a note        | P1       |
// | LG-09 | Edge     | With the guard off nothing is blocked or cut                         | P1       |
// | LG-10 | Happy    | A line or phrase written times in a row is found, at its shortest    | P0       |
// | LG-11 | Edge     | Rules, borders, numbered or scattered repeats are not loops          | P0       |
// | LG-12 | Edge     | Text and reasoning are watched apart                                 | P2       |
// | LG-13 | Edge     | A cut after tool results adds its own note, as a new user message    | P1       |
// | LG-14 | Edge     | Dropping and compacting never take a loop note for the user's prompt | P0       |
// | LG-15 | Edge     | A rerun after a call that may change things is not a loop            | P0       |
// | LG-16 | Edge     | Two calls sharing one id are each paired with their result           | P1       |
// | LG-17 | Edge     | A cancel during a cut returns the cancel and keeps no reply          | P1       |
// | LG-18 | Edge     | Non-ASCII rules, few-letter runs are not loops; pieces stay whole    | P0       |
// | LG-21 | Happy    | A short loop of few letters is caught after more copies              | P1       |
// | LG-22 | Edge     | The repeated piece is told from where its copies begin               | P2       |
// | LG-19 | Happy    | Repeated reasoning is cut like repeated text                         | P1       |
// | LG-20 | Security | A blocked repeat is never offered for approval                       | P1       |

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

type streamedReply struct {
	chunks    []string
	reasoning bool
	message   llm.Message
}

// streamingProvider streams each reply chunk by chunk and stops once the
// request is cancelled, unless it ignores the cancel.
type streamingProvider struct {
	replies      []streamedReply
	requests     []llm.Request
	cancelled    int
	ignoreCancel bool
}

func (p *streamingProvider) Stream(ctx context.Context, req llm.Request, onEvent func(llm.Event)) (llm.Response, error) {
	p.requests = append(p.requests, req)
	reply := p.replies[0]
	p.replies = p.replies[1:]
	for _, chunk := range reply.chunks {
		if ctx.Err() != nil && !p.ignoreCancel {
			p.cancelled++
			return llm.Response{}, ctx.Err()
		}
		kind := llm.EventText
		if reply.reasoning {
			kind = llm.EventReasoning
		}
		onEvent(llm.Event{Kind: kind, Text: chunk})
	}
	if reply.chunks != nil && reply.message.Text == "" {
		reply.message = llm.Message{Role: llm.RoleAssistant, Text: strings.Join(reply.chunks, "")}
	}
	return llm.Response{Message: reply.message}, nil
}

func callReply(name, args string) streamedReply {
	return streamedReply{message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("call_0", name, args)}}}
}

func textReply(text string) streamedReply {
	return streamedReply{message: llm.Message{Role: llm.RoleAssistant, Text: text}}
}

func guardedAgent(t *testing.T, provider llm.Provider) (*Agent, string) {
	t.Helper()
	agent, workDir := newAgentIn(t, provider)
	agent.Loop = LoopGuard{Enabled: true, Replies: 10, Repeats: 3, StreamRepeats: 5, Warnings: 2}
	return agent, workDir
}

func toolResults(history []llm.Message) []llm.Message {
	var results []llm.Message
	for _, message := range history {
		if message.Role == llm.RoleTool {
			results = append(results, message)
		}
	}
	return results
}

func TestRepeatedCalls(t *testing.T) {
	ctx := context.Background()

	t.Run("LG-01 the third same call with the same result is not run and says why", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{read, read, read, textReply("done")}}
		agent, _ := guardedAgent(t, provider)

		err := agent.Run(ctx, "read a.txt", Hooks{})

		results := toolResults(agent.History)
		if err != nil || len(results) != 3 || results[1].Text != "hello\n" {
			t.Fatalf("err = %v results = %+v", err, results)
		}
		if !results[2].IsError || !strings.Contains(results[2].Text, "going in circles") {
			t.Errorf("third result = %+v", results[2])
		}
	})

	t.Run("LG-02 the same call with a new result each time keeps running", func(t *testing.T) {
		count := callReply("bash", `{"command":"echo x >> n.txt; wc -l < n.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{count, count, count, count, textReply("done")}}
		agent, _ := guardedAgent(t, provider)

		err := agent.Run(ctx, "count", Hooks{})

		for _, result := range toolResults(agent.History) {
			if err != nil || result.IsError {
				t.Errorf("err = %v result = %+v", err, result)
			}
		}
	})

	t.Run("LG-03 arguments in another key order are the same call", func(t *testing.T) {
		first := callKey(toolCall("1", "grep", `{"pattern":"x","path":"."}`))
		second := callKey(toolCall("2", "grep", `{ "path": ".", "pattern": "x" }`))
		other := callKey(toolCall("3", "grep", `{"pattern":"y","path":"."}`))

		if first != second || first == other {
			t.Errorf("first = %q second = %q other = %q", first, second, other)
		}
	})

	t.Run("LG-04 calls older than the last replies window do not count", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		list := callReply("grep", `{"pattern":"nothing here"}`)
		provider := &streamingProvider{replies: []streamedReply{read, read, list, list, read, textReply("done")}}
		agent, _ := guardedAgent(t, provider)
		agent.Loop.Replies = 3
		agent.Loop.Repeats = 2

		_ = agent.Run(ctx, "read", Hooks{})

		results := toolResults(agent.History)
		if len(results) != 5 || !results[1].IsError || !results[3].IsError || results[4].IsError {
			t.Errorf("results = %+v", results)
		}
	})

	t.Run("LG-05 calls made before the user's last prompt do not count", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{read, read, textReply("one"), read, textReply("two")}}
		agent, _ := guardedAgent(t, provider)

		_ = agent.Run(ctx, "read", Hooks{})
		err := agent.Run(ctx, "read it again", Hooks{})

		for _, result := range toolResults(agent.History) {
			if err != nil || result.IsError {
				t.Errorf("err = %v result = %+v", err, result)
			}
		}
	})

	t.Run("LG-06 past the warnings the turn stops, every call still gets a result", func(t *testing.T) {
		thrice := streamedReply{message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			toolCall("call_0", "read", `{"path":"a.txt"}`),
			toolCall("call_1", "read", `{"path":"a.txt"}`),
			toolCall("call_2", "read", `{"path":"a.txt"}`),
		}}}
		provider := &streamingProvider{replies: []streamedReply{thrice, thrice}}
		agent, _ := guardedAgent(t, provider)
		agent.Loop.Warnings = 1

		err := agent.Run(ctx, "read", Hooks{})

		results := toolResults(agent.History)
		if !errors.Is(err, ErrLooping) || len(results) != 6 {
			t.Fatalf("err = %v results = %d", err, len(results))
		}
		if !strings.Contains(results[2].Text, "going in circles") || !strings.Contains(results[3].Text, "going in circles") || results[5].Text != stoppedCallResult {
			t.Errorf("results = %+v", results)
		}
	})
}

func TestRepeatedStream(t *testing.T) {
	ctx := context.Background()
	looping := streamedReply{chunks: []string{"Let me check the file.\n"}}
	for range 20 {
		looping.chunks = append(looping.chunks, "I will read the config now.\n")
	}

	t.Run("LG-07 a reply that repeats itself is cut, left out and the model is told", func(t *testing.T) {
		provider := &streamingProvider{replies: []streamedReply{looping, textReply("fixed")}}
		agent, _ := guardedAgent(t, provider)
		var cut string

		err := agent.Run(ctx, "go", Hooks{OnLoopCut: func(repeated string) { cut = repeated }})

		if err != nil || provider.cancelled != 1 || cut != "I will read the config now.\n" {
			t.Fatalf("err = %v cancelled = %d cut = %q", err, provider.cancelled, cut)
		}
		prompt := agent.History[0]
		if len(agent.History) != 2 || !strings.HasPrefix(prompt.Text, "go\n\n[mo loop guard]") || !strings.Contains(prompt.Text, "«I will read the config now.»") {
			t.Errorf("history = %+v", agent.History)
		}
		if agent.History[1].Text != "fixed" {
			t.Errorf("last = %+v", agent.History[1])
		}
	})

	t.Run("LG-08 a stream loop past the warnings stops the turn without a note", func(t *testing.T) {
		provider := &streamingProvider{replies: []streamedReply{looping, looping}}
		agent, _ := guardedAgent(t, provider)
		agent.Loop.Warnings = 1

		err := agent.Run(ctx, "go", Hooks{})

		if !errors.Is(err, ErrLooping) || len(agent.History) != 1 || strings.Count(agent.History[0].Text, loopNotePrefix) != 1 {
			t.Errorf("err = %v history = %+v", err, agent.History)
		}
	})

	t.Run("LG-09 with the guard off nothing is blocked or cut", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{read, read, read, looping}}
		agent, workDir := guardedAgent(t, provider)
		agent.Loop.Enabled = false

		err := agent.Run(ctx, "go", Hooks{})

		for _, result := range toolResults(agent.History) {
			if result.IsError {
				t.Errorf("result = %+v", result)
			}
		}
		if _, statErr := os.Stat(filepath.Join(workDir, "a.txt")); err != nil || provider.cancelled != 0 || statErr != nil {
			t.Errorf("err = %v cancelled = %d", err, provider.cancelled)
		}
	})

}

func TestRepeatWatch(t *testing.T) {
	feed := func(times int, chunks ...string) string {
		watch := newRepeatWatch(times)
		for _, chunk := range chunks {
			if found := watch.add(llm.Event{Kind: llm.EventText, Text: chunk}); found != "" {
				return found
			}
		}
		return ""
	}

	t.Run("LG-10 a line or phrase written times in a row is found, at its shortest", func(t *testing.T) {
		line := feed(3, "intro\n", "the same line again\n", "the same line again\n", "the same line again\n")
		phrase := feed(4, strings.Repeat("no wait, let me think ", 4))

		if line != "the same line again\n" || phrase != "no wait, let me think " {
			t.Errorf("line = %q phrase = %q", line, phrase)
		}
	})

	t.Run("LG-11 rules, borders, numbered or scattered repeats are not loops", func(t *testing.T) {
		rule := feed(3, strings.Repeat("=", 400))
		border := feed(3, strings.Repeat("|                    |\n", 10))
		var numbered strings.Builder
		for i := range 30 {
			fmt.Fprintf(&numbered, "%d. check the item\n", i)
		}
		listed := feed(3, numbered.String())
		scattered := feed(3, "the same line again\n", "other\n", "the same line again\n", "more\n", "the same line again\n")
		fewer := feed(5, strings.Repeat("the same line again\n", 4))

		for name, found := range map[string]string{"rule": rule, "border": border, "numbered": listed, "scattered": scattered, "fewer": fewer} {
			if found != "" {
				t.Errorf("%s found %q", name, found)
			}
		}
	})

	t.Run("LG-12 text and reasoning are watched apart", func(t *testing.T) {
		watch := newRepeatWatch(2)
		watch.add(llm.Event{Kind: llm.EventText, Text: "the same line again\n"})

		found := watch.add(llm.Event{Kind: llm.EventReasoning, Text: "the same line again\n"})

		if found != "" {
			t.Errorf("found %q across kinds", found)
		}
	})
}

func TestLoopFixes(t *testing.T) {
	ctx := context.Background()
	looping := streamedReply{}
	for range 10 {
		looping.chunks = append(looping.chunks, "I will read the config now.\n")
	}

	t.Run("LG-13 a cut after tool results adds its own note, as a new user message", func(t *testing.T) {
		provider := &streamingProvider{replies: []streamedReply{callReply("read", `{"path":"a.txt"}`), looping, textReply("fixed")}}
		agent, _ := guardedAgent(t, provider)

		err := agent.Run(ctx, "go", Hooks{})

		note := agent.History[3]
		if err != nil || len(agent.History) != 5 || note.Role != llm.RoleUser || !isLoopNote(note.Text) || agent.History[0].Text != "go" {
			t.Errorf("err = %v history = %+v", err, agent.History)
		}
	})

	t.Run("LG-14 dropping and compacting never take a loop note for the user's prompt", func(t *testing.T) {
		agent, _ := guardedAgent(t, &streamingProvider{})
		agent.History = []llm.Message{
			{Role: llm.RoleUser, Text: "fix it"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("call_0", "read", `{"path":"a.txt"}`)}},
			{Role: llm.RoleTool, ToolCallID: "call_0", Text: "hello"},
			{Role: llm.RoleUser, Text: fmt.Sprintf(cutReplyNote, "«x»")},
		}

		if next, last := agent.nextTurnStart(), agent.lastUserMessage(); next != 0 || last != 0 {
			t.Errorf("next turn = %d last prompt = %d", next, last)
		}
	})

	t.Run("LG-15 a rerun after a call that may change things is not a loop", func(t *testing.T) {
		check := callReply("bash", `{"command":"cat a.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{
			check, callReply("write", `{"path":"b.txt","content":"1"}`),
			check, callReply("write", `{"path":"b.txt","content":"2"}`),
			check, textReply("done"),
		}}
		agent, _ := guardedAgent(t, provider)

		err := agent.Run(ctx, "go", Hooks{})

		for _, result := range toolResults(agent.History) {
			if err != nil || result.IsError {
				t.Errorf("err = %v result = %+v", err, result)
			}
		}
	})

	t.Run("LG-16 two calls sharing one id are each paired with their result", func(t *testing.T) {
		pair := streamedReply{message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			toolCall("x", "read", `{"path":"a.txt"}`),
			toolCall("x", "read", `{"path":"a.txt"}`),
		}}}
		provider := &streamingProvider{replies: []streamedReply{pair, textReply("done")}}
		agent, _ := guardedAgent(t, provider)

		_ = agent.Run(ctx, "go", Hooks{})

		for _, result := range toolResults(agent.History) {
			if result.IsError {
				t.Errorf("result = %+v", result)
			}
		}
	})

	t.Run("LG-17 a cancel during a cut returns the cancel and keeps no reply", func(t *testing.T) {
		provider := &streamingProvider{replies: []streamedReply{looping}, ignoreCancel: true}
		agent, _ := guardedAgent(t, provider)
		cancelled, cancel := context.WithCancel(ctx)
		defer cancel()
		events := 0

		err := agent.Run(cancelled, "go", Hooks{OnEvent: func(llm.Event) {
			events++
			if events == 5 {
				cancel()
			}
		}})

		if !errors.Is(err, context.Canceled) || len(agent.History) != 1 || agent.History[0].Text != "go" {
			t.Errorf("err = %v history = %+v", err, agent.History)
		}
	})

	t.Run("LG-18 non-ASCII rules, few-letter runs are not loops; pieces stay whole", func(t *testing.T) {
		quiet := map[string]string{
			"box rule":   strings.Repeat("═", 400),
			"full stops": strings.Repeat("。", 100),
			"zeros":      "[" + strings.Repeat("0, ", 60),
			"bytes":      strings.Repeat("0x00, ", 40),
			"base64":     strings.Repeat("A", 300),
		}
		for name, text := range quiet {
			if found := repeatedEnd(text, 5); found != "" {
				t.Errorf("%s found %q", name, found)
			}
		}
		found := repeatedEnd(strings.Repeat("我需要再检查一下配置文件。", 5), 5)
		if found != "我需要再检查一下配置文件。" {
			t.Errorf("found %q", found)
		}
		if quoted := Quote("\x80\x80abc"); quoted != "«abc»" {
			t.Errorf("quoted %q", quoted)
		}
	})

	t.Run("LG-19 repeated reasoning is cut like repeated text", func(t *testing.T) {
		thinking := looping
		thinking.reasoning = true
		provider := &streamingProvider{replies: []streamedReply{thinking, textReply("fixed")}}
		agent, _ := guardedAgent(t, provider)

		err := agent.Run(ctx, "go", Hooks{})

		if err != nil || provider.cancelled != 1 || !strings.Contains(agent.History[0].Text, loopNotePrefix) {
			t.Errorf("err = %v cancelled = %d history = %+v", err, provider.cancelled, agent.History)
		}
	})

	t.Run("LG-20 a blocked repeat is never offered for approval", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{read, read, read, textReply("done")}}
		agent, _ := guardedAgent(t, provider)
		agent.ConfirmTools = []string{"read"}
		asked := 0

		_ = agent.Run(ctx, "go", Hooks{Approve: func(context.Context, ApprovalRequest) Decision {
			asked++
			return Allow
		}})

		if asked != 2 {
			t.Errorf("asked %d times", asked)
		}
	})

	t.Run("LG-21 a short loop of few letters is caught after more copies", func(t *testing.T) {
		early := repeatedEnd(strings.Repeat("Wait, ", 40), 5)
		late := repeatedEnd(strings.Repeat("Wait, ", 70), 5)
		laughing := repeatedEnd(strings.Repeat("ha ha ", 70), 5)

		if early != "" || late == "" || laughing == "" {
			t.Errorf("early = %q late = %q laughing = %q", early, late, laughing)
		}
	})

	t.Run("LG-22 the repeated piece is told from where its copies begin", func(t *testing.T) {
		watch := newRepeatWatch(3)
		stream := strings.Repeat("I will read the config now.\n", 4)
		found := ""
		for start := 0; start < len(stream) && found == ""; start += 3 {
			found = watch.add(llm.Event{Kind: llm.EventText, Text: stream[start:min(start+3, len(stream))]})
		}

		if found != "I will read the config now.\n" {
			t.Errorf("found %q", found)
		}
	})
}
