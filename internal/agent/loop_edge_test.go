package agent

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | LE-01 | Edge     | Checks that fail with no output, for other patterns, are no loop         | P0       |
// | LE-29 | Edge     | Other calls with the same output are not the same call                   | P0       |
// | LE-03 | Edge     | Polling a job is never stopped, whatever it shows                        | P0       |
// | LE-04 | Edge     | The same sentence in many replies of a batch job is no loop             | P0       |
// | LE-06 | Edge     | The same file read at other offsets is no loop                           | P1       |
// | LE-07 | Happy    | A call wrapped in tool_call is the call it stands for                    | P0       |
// | LE-08 | Happy    | The same path written in three ways is one call                          | P0       |
// | LE-09 | Happy    | A cycle of five calls is caught                                          | P1       |
// | LE-10 | Edge     | A cycle longer than the replies window is not seen (known limit)         | P2       |
// | LE-11 | Happy    | A cycle that goes on past the warnings stops the turn                    | P0       |
// | LE-12 | Edge     | A change after a block lets the call run again                           | P0       |
// | LE-13 | Edge     | With repeats 2 the second same call is not run                           | P1       |
// | LE-14 | Edge     | With the guard off nothing stops, not even by steps                      | P1       |
// | LE-15 | Edge     | With no warnings the first loop stops the turn                           | P1       |
// | LE-16 | Edge     | A subagent's own steps win over the guard's                              | P1       |
// | LE-17 | Happy    | A model whose calls change each time ends at the steps                   | P0       |
// | LE-19 | Edge     | The same file written thrice with the same content is a loop             | P1       |
// | LE-20 | Edge     | Reading again after a compaction is no loop                              | P0       |
// | LE-21 | Edge     | Results of a blocked call are never taken for the call's real result     | P1       |
// | LE-23 | Edge     | A shell command may be run more times than other calls                   | P1       |
// | LE-25 | Edge     | Counters and reworded text are not caught by the stream (known limit)    | P2       |
// | LE-05 | Edge     | What a call writes in its arguments is never cut                         | P0       |
// | LE-26 | Happy    | A loop that starts after normal text is found from where it begins       | P1       |
// | LE-30 | Edge     | web_fetch, and fetch as it was named, never change anything              | P1       |
// | LE-27 | Happy    | With early calls the same call is not run the third time                 | P0       |

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func windowOf(calls ...loopCall) []loopCall {
	for i := range calls {
		if calls[i].reply == 0 {
			calls[i].reply = len(calls) - i
		}
	}
	return calls
}

func TestLoopEdges(t *testing.T) {
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
	none := func(flags []bool) bool { return !slices.Contains(flags, true) }

	t.Run("LE-01 checks that fail with no output, for other patterns, are no loop", func(t *testing.T) {
		check := func(pattern string) streamedReply { return bashReply("grep -q " + pattern + " a.txt") }

		agent, flags, _ := run(t, nil, check("one"), check("two"), check("three"), check("four"))

		if !none(flags) {
			t.Errorf("blocked = %v results = %+v", flags, toolResults(agent.History))
		}
	})

	t.Run("LE-23 a shell command may be run more times than other calls", func(t *testing.T) {
		health := bashReply("echo up")

		_, flags, _ := run(t, nil, health, health, health, health, health, health)

		if want := []bool{false, false, false, false, true, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-29 other calls with the same output are not the same call", func(t *testing.T) {
		_, flags, err := run(t, nil,
			callReply("read", `{"path":"a.txt"}`),
			bashReply("cat a.txt"),
			bashReply("cat ./a.txt"),
			bashReply("head -n 1 a.txt"),
			callReply("grep", `{"pattern":"hello","path":"a.txt"}`),
			bashReply("tail a.txt"))

		if err != nil || !none(flags) {
			t.Errorf("err = %v blocked = %v", err, flags)
		}
	})

	t.Run("LE-03 polling a job is never stopped, whatever it shows", func(t *testing.T) {
		poll := callReply("bash", `{"id":1}`)

		_, flags, err := run(t, nil, poll, poll, poll, poll, poll)

		if err != nil || !none(flags) {
			t.Errorf("err = %v blocked = %v", err, flags)
		}
	})

	t.Run("LE-04 the same sentence in many replies of a batch job is no loop", func(t *testing.T) {
		say := func(n int) streamedReply {
			reply := bashReply(fmt.Sprintf("echo file%d", n))
			reply.message.Text = "Now I apply the same change to the next file of the list, as I did for the files before it."
			return reply
		}

		cut := 0
		provider := &streamingProvider{replies: []streamedReply{say(1), say(2), say(3), say(4), textReply("all done")}}
		agent, _ := guardedAgent(t, provider)
		err := agent.Run(ctx, "go", Hooks{OnLoopCut: func(string) { cut++ }})

		if err != nil || cut != 0 || len(toolResults(agent.History)) != 4 {
			t.Errorf("err = %v cut = %d", err, cut)
		}
	})

	t.Run("LE-06 the same file read at other offsets is no loop", func(t *testing.T) {
		page := func(offset int) streamedReply {
			return callReply("read", fmt.Sprintf(`{"path":"a.txt","offset":%d}`, offset))
		}

		_, flags, _ := run(t, nil, page(1), page(2), page(3), page(4), page(5))

		if !none(flags) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-07 a call wrapped in tool_call is the call it stands for", func(t *testing.T) {
		wrapped := callReply("tool_call", `{"name":"read","args":{"path":"a.txt"}}`)

		_, flags, _ := run(t, nil, wrapped, wrapped, wrapped)

		if want := []bool{false, false, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-08 the same path written in three ways is one call", func(t *testing.T) {
		_, flags, _ := run(t, nil,
			callReply("read", `{"path":"a.txt"}`),
			callReply("read", `{"path":"./a.txt","offset":0}`),
			callReply("read", `{"path":"sub/../a.txt"}`))

		if want := []bool{false, false, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-09 a cycle of five calls is caught", func(t *testing.T) {
		cycle := []streamedReply{callReply("read", `{"path":"a.txt"}`)}
		for _, name := range []string{"b", "c", "d", "e"} {
			cycle = append(cycle, callReply("write", fmt.Sprintf(`{"path":"%s.txt","content":"x"}`, name)))
		}

		_, flags, _ := run(t, func(a *Agent) { a.Loop.Replies = 11 }, slices.Concat(cycle, cycle, cycle[:1])...)

		if !flags[10] || slices.Contains(flags[:10], true) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-10 a cycle longer than the replies window is not seen (known limit)", func(t *testing.T) {
		var cycle []streamedReply
		for n := range 12 {
			cycle = append(cycle, bashReply(fmt.Sprintf("echo %d", n)))
		}

		_, flags, _ := run(t, nil, slices.Concat(cycle, cycle, cycle[:1])...)

		if !none(flags) {
			t.Errorf("blocked = %v: if this is seen now, drop the known limit from docs", flags)
		}
	})

	t.Run("LE-11 a cycle that goes on past the warnings stops the turn", func(t *testing.T) {
		a, b := callReply("read", `{"path":"a.txt"}`), callReply("write", `{"path":"b.txt","content":"x"}`)

		agent, flags, err := run(t, nil, a, b, a, b, a, b, a, b, a, b)

		if !errors.Is(err, ErrLooping) || !slices.Contains(flags, true) {
			t.Errorf("err = %v blocked = %v calls = %d", err, flags, len(toolResults(agent.History)))
		}
	})

	t.Run("LE-12 a change after a block lets the call run again", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		write := callReply("write", `{"path":"a.txt","content":"new"}`)

		_, flags, err := run(t, nil, read, read, read, write, read)

		if err != nil || !flags[2] || flags[4] {
			t.Errorf("err = %v blocked = %v", err, flags)
		}
	})

	t.Run("LE-13 with repeats 2 the second same call is not run", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)

		_, flags, _ := run(t, func(a *Agent) { a.Loop.Repeats = 2 }, read, read)

		if want := []bool{false, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-14 with the guard off nothing stops, not even by steps", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)

		_, flags, err := run(t, func(a *Agent) { a.Loop = LoopGuard{Steps: 2} }, read, read, read, read, read)

		if err != nil || !none(flags) {
			t.Errorf("err = %v blocked = %v", err, flags)
		}
	})

	t.Run("LE-15 with no warnings the first loop stops the turn", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)

		_, flags, err := run(t, func(a *Agent) { a.Loop.Warnings = 0 }, read, read, read, read)

		if !errors.Is(err, ErrLooping) || !flags[2] {
			t.Errorf("err = %v blocked = %v", err, flags)
		}
	})

	t.Run("LE-16 a subagent's own steps win over the guard's", func(t *testing.T) {
		var steps []streamedReply
		for n := range 6 {
			steps = append(steps, bashReply(fmt.Sprintf("echo %d", n)))
		}

		agent, _, err := run(t, func(a *Agent) { a.MaxSteps, a.Loop.Steps = 2, 100 }, steps...)

		if !errors.Is(err, ErrStepLimit) || len(toolResults(agent.History)) != 2 {
			t.Errorf("err = %v results = %d", err, len(toolResults(agent.History)))
		}
	})

	t.Run("LE-17 a model whose calls change each time ends at the steps", func(t *testing.T) {
		var steps []streamedReply
		for n := range 12 {
			steps = append(steps, bashReply(fmt.Sprintf("echo %d >> n.txt; wc -l < n.txt", n)))
		}

		agent, flags, err := run(t, func(a *Agent) { a.Loop.Steps = 8 }, steps...)

		if !errors.Is(err, ErrStepLimit) || len(toolResults(agent.History)) != 8 || !none(flags) {
			t.Errorf("err = %v results = %d blocked = %v", err, len(toolResults(agent.History)), flags)
		}
	})

	t.Run("LE-19 the same file written thrice with the same content is a loop", func(t *testing.T) {
		write := callReply("write", `{"path":"b.txt","content":"same"}`)

		_, flags, _ := run(t, nil, write, write, write)

		if want := []bool{false, false, true}; !slices.Equal(flags, want) {
			t.Errorf("blocked = %v", flags)
		}
	})

	t.Run("LE-20 reading again after a compaction is no loop", func(t *testing.T) {
		read := callReply("read", `{"path":"a.txt"}`)
		provider := &streamingProvider{replies: []streamedReply{read, read, textReply("one"), read, read, textReply("two")}}
		agent, _ := guardedAgent(t, provider)
		_ = agent.Run(ctx, "first", Hooks{})
		agent.History = withSummary("it read the file twice", nil, "")

		err := agent.Run(ctx, "again", Hooks{})

		if err != nil || !none(blocked(toolResults(agent.History))) {
			t.Errorf("err = %v history = %+v", err, agent.History)
		}
	})
	t.Run("LE-21 results of a blocked call are never taken for the call's real result", func(t *testing.T) {
		key := callKey(toolCallOf("read", `{"path":"a.txt"}`))
		calls := windowOf(
			loopCall{tool: "read", key: key, output: loopNotePrefix + "Not run", isError: true},
			loopCall{tool: "read", key: key, output: "hello"},
			loopCall{tool: "read", key: key, output: "hello"},
		)

		if earlier := earlierRuns(calls, toolCallOf("read", `{"path":"a.txt"}`)); earlier != 3 {
			t.Errorf("earlier = %d", earlier)
		}
	})

}

func toolCallOf(name, args string) llm.ToolCall { return toolCall("x", name, args) }

func TestLoopKnownLimits(t *testing.T) {
	t.Run("LE-25 counters and reworded text are not caught by the stream (known limit)", func(t *testing.T) {
		var counted, reworded strings.Builder
		for n := range 40 {
			fmt.Fprintf(&counted, "Step %d: I will check the configuration file again.\n", n)
		}
		for _, line := range []string{"Let me look at the file once more.", "I should read the file one more time.", "Reading that file again now.", "Time to open the file again."} {
			reworded.WriteString(strings.Repeat(line+"\n", 1))
		}

		if found := repeatedEnd(counted.String(), 5); found != "" {
			t.Errorf("counted: %q; if caught now, drop the known limit from docs", found)
		}
		if found := repeatedEnd(strings.Repeat(reworded.String(), 1), 5); found != "" {
			t.Errorf("reworded: %q", found)
		}
	})

	t.Run("LE-05 what a call writes in its arguments is never cut", func(t *testing.T) {
		watch := newRepeatWatch(5)
		rows := strings.Repeat(`{"name":"same","kind":"fixture"},\n`, 40)
		flags := strings.Repeat(`--exclude-dir=node_modules `, 20)

		for _, text := range []string{rows, flags} {
			if found := watch.add(llm.Event{Kind: llm.EventToolCall, Call: 0, Text: text}); found != "" {
				t.Errorf("found %q", found)
			}
		}
	})

	t.Run("LE-26 a loop that starts after normal text is found from where it begins", func(t *testing.T) {
		watch := newRepeatWatch(5)
		stream := "Let me think about how to fix the failing build before I touch anything.\n" + strings.Repeat("I will read the config now.\n", 8)
		found := feed(watch, llm.EventText, stream)

		if found != "I will read the config now.\n" {
			t.Errorf("found %q", found)
		}
	})
}

func TestFetchIsQuiet(t *testing.T) {
	t.Run("LE-30 web_fetch, and fetch as it was named, never change anything", func(t *testing.T) {
		agent, _ := guardedAgent(t, &streamingProvider{})

		for _, name := range []string{"web_fetch", "fetch", "web_search"} {
			if agent.mayChange(toolCallOf(name, `{"url":"https://go.dev"}`)) {
				t.Errorf("%s may change something", name)
			}
		}
	})
}

func TestLoopWithEarlyCalls(t *testing.T) {
	ctx := context.Background()

	t.Run("LE-27 with early calls the same call is not run the third time", func(t *testing.T) {
		read := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("r", "read", `{"path":"a.txt"}`)}}
		provider := &piecedProvider{replies: []llm.Message{read, read, read, {Role: llm.RoleAssistant, Text: "done"}}}
		agent, _ := earlyAgent(t, provider)
		agent.Loop = LoopGuard{Enabled: true, Replies: 10, Repeats: 3, StreamRepeats: 5, Warnings: 2}

		err := agent.Run(ctx, "go", Hooks{})

		if flags := blocked(toolResults(agent.History)); err != nil || !slices.Equal(flags, []bool{false, false, true}) {
			t.Errorf("err = %v blocked = %v", err, flags)
		}
	})

}
