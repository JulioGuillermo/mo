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
// | LG-21 | Happy    | A short loop of few letters is caught after a thousand characters    | P1       |
// | LG-22 | Edge     | The repeated piece is told from where its copies begin               | P2       |
// | LG-19 | Happy    | Repeated reasoning is cut like repeated text                         | P1       |
// | LG-20 | Security | A blocked repeat is never offered for approval                       | P1       |
// | LG-23 | Edge     | A cycle of commands with the same results is caught                  | P0       |
// | LG-24 | Edge     | A chain of several calls written again and again is caught           | P0       |
// | LG-25 | Edge     | A call in a cycle whose result changes is a look at a state          | P0       |
// | LG-26 | Edge     | Results that change once and then stay are a loop                    | P1       |
// | LG-27 | Edge     | Results that differ in a time or a duration are not the same         | P0       |
// | LG-28 | Edge     | The same tool failing the same way with other args is no loop        | P0       |
// | LG-29 | Happy    | The same failing call is a loop like any other                       | P0       |
// | LG-30 | Edge     | The same call written another way is the same call                   | P1       |
// | LG-31 | Happy    | The main agent stops at its steps                                    | P1       |
// | LG-34 | Edge     | A piece over 2000 bytes written again and again is found             | P2       |
// | LG-35 | Edge     | A compaction starts the count again                                   | P1       |
// | LG-36 | Edge     | Jobs that end do not reset the warnings                              | P1       |
// | LG-37 | Edge     | A summary that repeats itself is cut                                 | P2       |
// | LG-38 | Happy    | A single character written again and again is caught                 | P0       |
// | LG-39 | Edge     | A single character, not enough yet, is quiet                         | P0       |
// | LG-40 | Edge     | A short phrase that is written in places, not in a row, is quiet     | P0       |
// | LG-41 | Edge     | A rule of 80 columns is quiet                                        | P0       |
// | LG-42 | Happy    | A paragraph of 3500 bytes written five times is caught               | P1       |
// | LG-43 | Edge     | The same paragraph four times is quiet                               | P1       |
// | LG-44 | Happy    | Newlines without end are caught                                      | P1       |
// | LG-45 | Happy    | A list of characters repeated on one line is caught                  | P1       |
// | LG-46 | Happy    | A piece of 60 KB written five times is caught                         | P1       |
// | LG-47 | Happy    | A loop after megabytes of other text is caught                       | P1       |

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
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

	t.Run("LG-21 a short loop of few letters is caught after a thousand characters", func(t *testing.T) {
		early := repeatedEnd(strings.Repeat("Wait, ", 100), 5)
		late := repeatedEnd(strings.Repeat("Wait, ", 200), 5)
		laughing := repeatedEnd(strings.Repeat("ha ha ", 200), 5)

		if early != "" || late != "Wait, " || laughing != "ha " {
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

func bashReply(command string) streamedReply {
	return callReply("bash", fmt.Sprintf(`{"command":%q}`, command))
}

func blocked(results []llm.Message) []bool {
	flags := make([]bool, len(results))
	for i, result := range results {
		flags[i] = result.IsError && isLoopNote(result.Text)
	}
	return flags
}

func TestLoopGaps(t *testing.T) {
	ctx := context.Background()
	run := func(t *testing.T, tune func(*Agent), replies ...streamedReply) (*Agent, []bool, error) {
		t.Helper()
		provider := &streamingProvider{replies: append(replies, textReply("done"))}
		agent, _ := guardedAgent(t, provider)
		if tune != nil {
			tune(agent)
		}
		err := agent.Run(ctx, "go", Hooks{})
		return agent, blocked(toolResults(agent.History)), err
	}

	t.Run("LG-23 a cycle of two calls with the same results is caught", func(t *testing.T) {
		look, save := callReply("read", `{"path":"a.txt"}`), callReply("write", `{"path":"b.txt","content":"x"}`)

		_, flags, _ := run(t, nil, look, save, look, save, look)

		if want := []bool{false, false, false, false, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LG-24 a chain of several calls repeated is caught", func(t *testing.T) {
		look := callReply("read", `{"path":"a.txt"}`)
		save := func(name string) streamedReply {
			return callReply("write", fmt.Sprintf(`{"path":"%s.txt","content":"x"}`, name))
		}
		b, c, d := save("b"), save("c"), save("d")

		_, flags, _ := run(t, nil, look, b, c, d, look, b, c, d, look)

		if flags[7] || !flags[8] || slices.Contains(flags[:8], true) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LG-25 a call in a cycle whose result changes is a look at some state", func(t *testing.T) {
		count, other := bashReply("echo x >> n.txt; wc -l < n.txt"), bashReply("echo b")

		_, flags, _ := run(t, nil, count, other, count, other, count, other, count)

		if slices.Contains(flags, true) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LG-26 results that change and then stay are a loop", func(t *testing.T) {
		call := toolCallOf("read", `{"path":"n.txt"}`)
		key := callKey(call)
		plateau := windowOf(loopCall{key: key, output: "3"}, loopCall{key: key, output: "3"}, loopCall{key: key, output: "2"}, loopCall{key: key, output: "1"})
		moving := windowOf(loopCall{key: key, output: "3"}, loopCall{key: key, output: "2"}, loopCall{key: key, output: "3"})

		if earlierRuns(plateau, call) != 2 || earlierRuns(moving, call) != 1 {
			t.Errorf("plateau = %d moving = %d", earlierRuns(plateau, call), earlierRuns(moving, call))
		}
	})

	t.Run("LG-27 results that differ in a time or a duration are not the same result", func(t *testing.T) {
		call := toolCallOf("read", `{"path":"a.txt"}`)
		key := callKey(call)
		calls := windowOf(
			loopCall{key: key, output: "ok  \tmo/agent\t0.321s"},
			loopCall{key: key, output: "ok  \tmo/agent\t1.9s"},
			loopCall{key: key, output: "ok  \tmo/agent\t0.5s"},
		)

		if earlier := earlierRuns(calls, call); earlier != 1 {
			t.Errorf("earlier = %d", earlier)
		}
	})

	t.Run("LG-28 the same tool failing the same way with other arguments is no loop", func(t *testing.T) {
		edit := func(old string) streamedReply {
			return callReply("edit", fmt.Sprintf(`{"path":"a.txt","old":%q,"new":"x"}`, old))
		}

		agent, flags, _ := run(t, nil, edit("one"), edit("two"), edit("three"), edit("four"))

		if slices.Contains(flags, true) || !toolResults(agent.History)[3].IsError {
			t.Errorf("blocked = %v results = %+v", flags, toolResults(agent.History))
		}
	})

	t.Run("LG-29 the same failing call is a loop like any other", func(t *testing.T) {
		bad := callReply("edit", `{"path":"a.txt","old":"nothing","new":"x"}`)

		_, flags, _ := run(t, nil, bad, bad, bad)

		if want := []bool{false, false, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LG-30 the same call written another way is the same call", func(t *testing.T) {
		first := callKey(toolCall("1", "read", `{"path":"a.txt"}`))
		second := callKey(toolCall("2", "read", `{"path":"./a.txt","offset":0,"limit":null}`))
		other := callKey(toolCall("3", "read", `{"path":"b.txt"}`))

		if first != second || first == other {
			t.Errorf("first = %q second = %q other = %q", first, second, other)
		}
	})

	t.Run("LG-31 the main agent stops at its steps", func(t *testing.T) {
		check := func(n int) streamedReply { return bashReply(fmt.Sprintf("echo %d", n)) }

		agent, _, err := run(t, func(a *Agent) { a.Loop.Steps = 3 }, check(1), check(2), check(3), check(4))

		if !errors.Is(err, ErrStepLimit) || len(toolResults(agent.History)) != 3 {
			t.Errorf("err = %v results = %d", err, len(toolResults(agent.History)))
		}
	})

	t.Run("LG-34 a piece over 2000 bytes written again and again is found", func(t *testing.T) {
		var sentences strings.Builder
		for n := range 70 {
			fmt.Fprintf(&sentences, "Sentence number %d says something about topic %d. ", n, n*7)
		}
		paragraph := sentences.String()

		found := repeatedEnd(strings.Repeat(paragraph, 5), 5)

		if found != paragraph {
			t.Errorf("found %d bytes", len(found))
		}
	})

	t.Run("LG-35 a compaction starts the count again", func(t *testing.T) {
		agent, _ := guardedAgent(t, &streamingProvider{})
		read := toolCallOf("read", `{"path":"a.txt"}`)
		agent.History = []llm.Message{{Role: llm.RoleUser, Text: "go"}}
		for _, id := range []string{"1", "2"} {
			call := read
			call.ID = id
			agent.History = append(agent.History,
				llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
				llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: "hello"})
		}
		before := earlierRuns(agent.window(), read)
		agent.History = withSummary("it read the file twice", nil, "go")

		if after := earlierRuns(agent.window(), read); before != 2 || after != 0 {
			t.Errorf("before = %d after = %d", before, after)
		}
	})

	t.Run("LG-36 jobs that end do not reset the warnings", func(t *testing.T) {
		agent, _ := guardedAgent(t, &streamingProvider{})
		agent.loops = 2

		_ = agent.RunEndedJobs(ctx, Hooks{})

		if agent.loops != 2 {
			t.Errorf("loops = %d", agent.loops)
		}
	})

	t.Run("LG-37 a summary that repeats itself is cut", func(t *testing.T) {
		looping := streamedReply{}
		for range 20 {
			looping.chunks = append(looping.chunks, "The user asked to fix the build.\n")
		}
		provider := &streamingProvider{replies: []streamedReply{looping}}
		agent, _ := guardedAgent(t, provider)

		_, err := agent.summarize(ctx, []llm.Message{{Role: llm.RoleUser, Text: "go"}}, "")

		if err == nil || !strings.Contains(err.Error(), "kept repeating") || provider.cancelled != 1 {
			t.Errorf("err = %v cancelled = %d", err, provider.cancelled)
		}
	})
}

func TestRepeatShapes(t *testing.T) {
	var sentences strings.Builder
	for n := range 70 {
		fmt.Fprintf(&sentences, "Sentence number %d says something about topic %d. ", n, n*7)
	}
	paragraph := sentences.String()
	var blob strings.Builder
	for n := range 1200 {
		fmt.Fprintf(&blob, "line %d of a big block of text that goes on and on. ", n)
	}

	cases := []struct {
		name  string
		text  string
		found bool
	}{
		{"LG-38 a single character written again and again", strings.Repeat("a", 1200), true},
		{"LG-39 a single character, not enough yet", strings.Repeat("a", 900), false},
		{"LG-40 hola hola with the code and the answer around it", "hola hola\nprint(\"hola hola\")\nperfecto, da hola hola\n", false},
		{"LG-41 a rule of 80 columns", strings.Repeat("=", 80) + "\n", false},
		{"LG-42 a paragraph of 3500 bytes written five times", strings.Repeat(paragraph, 5), true},
		{"LG-43 the same paragraph only four times", strings.Repeat(paragraph, 4), false},
		{"LG-44 newlines without end", strings.Repeat("\n", 1500), true},
		{"LG-45 a chars list repeated on one line", "[" + strings.Repeat("'a', 'b', 'c', ", 120), true},
		{"LG-46 a huge piece written five times", strings.Repeat(blob.String(), 5), true},
		{"LG-47 a loop after megabytes of other text", func() string {
			random := rand.New(rand.NewSource(1))
			other := make([]byte, 2_300_000)
			for i := range other {
				other[i] = "abcdefghijklmnopqrstuvwxyz  \n"[random.Intn(29)]
			}
			return string(other) + strings.Repeat("I will read the config now.\n", 8)
		}(), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := newRepeatWatch(5)
			found := ""
			for start := 0; start < len(c.text) && found == ""; start += 97 {
				found = watch.add(llm.Event{Kind: llm.EventText, Text: c.text[start:min(start+97, len(c.text))]})
			}
			if (found != "") != c.found {
				t.Errorf("found = %q (%d bytes), want found = %v", Quote(found), len(found), c.found)
			}
		})
	}
}
