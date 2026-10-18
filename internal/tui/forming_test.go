package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | LT-01 | Happy    | A field is read from JSON cut short, escapes decoded, as far as it came | P0    |
// | LT-02 | Happy    | A call shows its name as soon as it comes, then its command forming  | P0       |
// | LT-03 | Happy    | A file being written shows its path, line count and last lines; tool_call shows the tool | P1 |
// | LT-04 | Edge     | A whole call the reply ended with waits in view until it runs, then goes | P0    |
// | LT-08 | Edge     | A call that ends streaming after the first started shows whole, waiting; the next reply starts anew | P0 |
// | LT-05 | Happy    | The spinner says what the agent waits on, compacting, and goes back to working | P1 |
// | LT-06 | Happy    | Each thing the agent does has its word and colour, in the spinner only | P1      |
// | LT-10 | Edge     | Once a call ran, the state follows the model again, reasoning or talking, not the call | P0 |
// | LT-09 | Happy    | Each tool says what it does and on what: the file, the pattern, the programs a command runs, once each | P1 |
// | LT-11 | Happy    | While the model writes a call the state is that call; once done, it waits for the tools | P0 |
// | LT-12 | Edge     | A running call is what the state says, and the calls behind it do not change that | P1 |
// | LT-13 | Happy    | Every program of a command is named, the trivial ones too; sudo, timeout and the like name theirs inside () | P1 |
// | LT-07 | Happy    | While llama-server reads the prompt, the state says how far, cache left out | P1   |

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/llm"
)

func streaming(m model, events ...llm.Event) model {
	for _, event := range events {
		updated, _ := m.Update(streamMsg(event))
		m = updated.(model)
	}
	return m
}

func piece(call int, name, text string) llm.Event {
	return llm.Event{Kind: llm.EventToolCall, Call: call, Name: name, Text: text}
}

func TestPartialJSON(t *testing.T) {
	t.Run("LT-01 a field is read from JSON cut short, escapes decoded, as far as it came", func(t *testing.T) {
		cases := []struct {
			json, key, want string
			found           bool
		}{
			{`{"command":"ls -la"}`, "command", "ls -la", true},
			{`{"command":"go te`, "command", "go te", true},
			{`{"content":"a\nb \"q\" café`, "content", "a\nb \"q\" café", true},
			{`{"content":"line\`, "content", "line", true},
			{`{"content":"x\u00`, "content", "x", true},
			{`{"path":"a.go","con`, "content", "", false},
			{`{"name":"bash","args":{"command":"make"`, "command", "make", true},
		}
		for _, c := range cases {
			got, found := partialString(c.json, c.key)
			if got != c.want || found != c.found {
				t.Errorf("%s in %q = %q, %v", c.key, c.json, got, found)
			}
		}
	})
}

func TestFormingCalls(t *testing.T) {
	t.Run("LT-02 a call shows its name as soon as it comes, then its command forming", func(t *testing.T) {
		m := conversationModel()
		m.running = true

		named := streaming(m, piece(0, "bash", ""))
		forming := streaming(named, piece(0, "", `{"command":"go test ./`), piece(0, "", `.. && echo ok`))

		if !strings.Contains(breathing(shown2(named)), "· bash") || !strings.Contains(shown2(forming), "go test ./.. && echo ok") {
			t.Errorf("named:\n%s\nforming:\n%s", shown2(named), shown2(forming))
		}
	})

	t.Run("LT-03 a file being written shows its path, line count and last lines; tool_call shows the tool", func(t *testing.T) {
		m := conversationModel()
		m.running = true
		var body strings.Builder
		for i := range 30 {
			body.WriteString("line " + string(rune('a'+i%26)) + "\\n")
		}

		writing := streaming(m, piece(0, "write", `{"path":"notes.md","content":"`+body.String()+`last line`))
		wrapped := streaming(m, piece(0, "tool_call", `{"name":"edit","args":{"path":"a.go","new":"x := 1`))

		view := breathing(shown2(writing))
		if !strings.Contains(view, "· write notes.md") || !strings.Contains(view, "writing · 31 lines") || !strings.Contains(view, "last line") || strings.Contains(view, "line a\n") && strings.Count(view, "line ") > 13 {
			t.Errorf("writing:\n%s", view)
		}
		if !strings.Contains(breathing(shown2(wrapped)), "· edit a.go") || !strings.Contains(shown2(wrapped), "x := 1") {
			t.Errorf("wrapped:\n%s", shown2(wrapped))
		}
	})

	t.Run("LT-08 a call that ends streaming after the first started shows whole, waiting; the next reply starts anew", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"sleep 12"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "bash", Text: `{"comm`})
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash"}})
		shown, _ := started.(model).Update(streamEndMsg{})
		m = streaming(shown.(model), llm.Event{Kind: llm.EventToolCall, Call: 1, Text: `and":"echo SECOND"}`})
		waiting := shown2(m)
		second, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c2", Name: "bash"}})
		next := streaming(second.(model), llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "d1", Name: "read", Text: `{"path":"x"}`})

		if !strings.Contains(waiting, "echo SECOND") || !strings.Contains(waiting, "waits for the call before it") {
			t.Errorf("waiting:\n%s", waiting)
		}
		if len(next.forming) != 1 || next.forming[0].name != "read" {
			t.Errorf("next reply's calls %+v", next.forming)
		}
	})

	t.Run("LT-04 a whole call the reply ended with waits in view until it runs, then goes", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"sleep 12"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "bash", Text: `{"command":"echo SECOND"}`})
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash"}})

		ended, _ := started.(model).Update(streamEndMsg{})
		waiting := shown2(ended.(model))
		second, _ := ended.(model).Update(toolStartMsg{call: llm.ToolCall{ID: "c2", Name: "bash"}})
		finished, _ := second.(model).finishRun(nil)

		if !strings.Contains(waiting, "echo SECOND") || !strings.Contains(waiting, "waits for the call before it") {
			t.Errorf("while the first runs:\n%s", waiting)
		}
		if strings.Contains(shown2(second.(model)), "waits for the call before it") || len(finished.(model).forming) != 0 {
			t.Errorf("once it runs:\n%s", shown2(second.(model)))
		}
	})
}

// shown2 is the conversation as drawn, live part included.
func shown2(m model) string {
	return ansi.Strip(m.conversation(40).View())
}

func TestActivity(t *testing.T) {
	t.Run("LT-05 the spinner says what the agent waits on, compacting, and goes back to working", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}

		compacting, _ := m.Update(activityMsg("compacting the conversation"))
		back, _ := compacting.(model).Update(activityMsg(""))
		done, _ := compacting.(model).finishRun(nil)

		if !strings.Contains(ansi.Strip(compacting.(model).bottom()), "compacting") || !strings.Contains(ansi.Strip(back.(model).bottom()), "waiting") || done.(model).activity != "" {
			t.Errorf("compacting:\n%s\nback:\n%s", ansi.Strip(compacting.(model).bottom()), ansi.Strip(back.(model).bottom()))
		}
	})
}

func TestStates(t *testing.T) {
	t.Run("LT-06 each thing the agent does has its word and colour, in the spinner only", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		colors := statePalette(true)
		cases := []struct {
			name  string
			setup func(model) model
			word  string
			tint  any
		}{
			{"waiting", func(m model) model { return m }, "waiting", colors.waiting},
			{"reasoning", func(m model) model { return streaming(m, llm.Event{Kind: llm.EventReasoning, Text: "hmm"}) }, "reasoning", colors.reasoning},
			{"talking", func(m model) model { return streaming(m, llm.Event{Kind: llm.EventText, Text: "Sure"}) }, "talking", colors.writing},
			{"reading", func(m model) model { return streaming(m, piece(0, "read", `{"path":"a.go"}`)) }, "reading", colors.reading},
			{"editing", func(m model) model { return streaming(m, piece(0, "edit", `{"path":"a.go"}`)) }, "editing", colors.changing},
			{"bash", func(m model) model { return streaming(m, piece(0, "bash", `{"command":"nmap -sV x"}`)) }, "running", colors.changing},
			{"prompt", func(m model) model {
				return streaming(m, llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 600, Total: 1000}})
			}, "prompt", colors.prompt},
			{"compacting", func(m model) model {
				updated, _ := m.Update(activityMsg("compacting the conversation"))
				return updated.(model)
			}, "compacting", colors.compacting},
		}
		for _, c := range cases {
			got := c.setup(m).currentState()
			if got.word != c.word || got.tint != c.tint {
				t.Errorf("%s: word = %q", c.name, got.word)
			}
			spinner, _ := c.setup(m).upperLayout()
			if !strings.Contains(ansi.Strip(spinner), c.word) {
				t.Errorf("%s: the spinner does not say %q", c.name, c.word)
			}
			if strings.Contains(ansi.Strip(c.setup(m).statusLine()), c.word) {
				t.Errorf("%s: the status line says %q too", c.name, c.word)
			}
		}
	})

	t.Run("LT-09 each tool says what it does and on what: the file, the pattern, the programs a command runs, once each", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		cases := map[string]string{
			`{"name":"edit","args":{"path":"main.go"}}`:                                               "editing · main.go",
			`{"name":"grep","args":{"pattern":"TODO"}}`:                                               "scanning · TODO",
			`{"name":"bash","args":{"command":"docker build . && docker push x && docker run -d x"}}`: "running · docker",
			`{"name":"bash","args":{"command":"cd x && go test ./... | tail -5"}}`:                    "running · cd, go, tail",
			`{"name":"bash","args":{"command":"cd x; make; curl -s y; jq . z"}}`:                      "running · cd, make, curl +1",
			`{"name":"bash","args":{"command":"sleep 30 && echo done"}}`:                              "running · sleep, echo",
			`{"name":"git","args":{"action":"commit","message":"x"}}`:                                 "versioning · commit",
			`{"name":"mcp__playwright__browser_click","args":{}}`:                                     "calling · playwright › browser_click",
		}
		for raw, want := range cases {
			var call struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			}
			_ = json.Unmarshal([]byte(raw), &call)
			got := m.toolState(call.Name, string(call.Args), statePalette(true))
			if got.word+" · "+got.detail != want {
				t.Errorf("%s: %q · %q, want %q", raw, got.word, got.detail, want)
			}
		}
	})

	t.Run("LT-13 every program of a command is named, the trivial ones too; sudo, timeout and the like name theirs inside ()", func(t *testing.T) {
		cases := map[string]string{
			"sudo ls /root":                           "sudo(ls)",
			"sudo -u postgres psql -c 'select 1'":     "sudo(psql)",
			"timeout 5 curl -s example.com":           "timeout(curl)",
			"timeout -s KILL 10 curl x":               "timeout(curl)",
			"sudo timeout 5 curl x":                   "sudo(timeout(curl))",
			"nice -n 10 make -j8":                     "nice(make)",
			"nohup ./run.sh &":                        "nohup(run.sh)",
			"env FOO=1 BAR=2 make":                    "env(make)",
			"FOO=1 make":                              "make",
			"xargs -n 1 echo":                         "xargs(echo)",
			"watch -n 2 kubectl get pods":             "watch(kubectl)",
			"stdbuf -oL tail -f log":                  "stdbuf(tail)",
			"sudo":                                    "sudo",
			"sudo -v":                                 "sudo",
			"env":                                     "env",
			"timeout 5":                               "timeout",
			"/usr/bin/sudo /bin/ls":                   "sudo(ls)",
			"cd /tmp && sudo rm -rf x; echo done":     "cd, sudo(rm), echo",
			"sudo ls | timeout 3 grep x | wc -l":      "sudo(ls), timeout(grep), wc",
			"echo a; echo b; pwd; true; printf x":     "echo, pwd, true +1",
			"sudo -- ls":                              "sudo(ls)",
			"sudo --user root ls":                     "sudo(ls)",
			"taskset 0x3 ./bench":                     "taskset(bench)",
			"bash -c 'sudo systemctl restart nginx'":  "sudo(systemctl)",
			"ssh -p 2222 user@host ls -la":            "ssh(ls)",
			"ssh host 'df -h'":                        "ssh(df)",
			"ssh host":                                "ssh",
			"docker exec -it web ls /app":             "docker exec(ls)",
			"docker exec -e A=1 web sh -c 'x'":        "docker exec(sh)",
			"docker run --rm -v a:/b ubuntu bash":     "docker run(bash)",
			"docker run -d nginx":                     "docker",
			"docker build . && docker push x":         "docker",
			"kubectl exec mypod -- ls /tmp":           "kubectl exec(ls)",
			"kubectl exec -n prod mypod -c app -- ps": "kubectl exec(ps)",
			"kubectl get pods":                        "kubectl",
			"uv run pytest -x":                        "uv run(pytest)",
			"uv sync":                                 "uv",
			"poetry run mypy .":                       "poetry run(mypy)",
			"npx -p typescript tsc --noEmit":          "npx(tsc)",
			"pnpm exec eslint .":                      "pnpm exec(eslint)",
			"bundle exec rspec":                       "bundle exec(rspec)",
			"parallel -j4 gzip ::: *.log":             "parallel(gzip)",
			"runuser -u www-data -- php artisan x":    "runuser(php)",
			"systemd-run --unit job make":             "systemd-run(make)",
			"flock /tmp/lock make":                    "flock(make)",
			"xvfb-run -a go test ./...":               "xvfb-run(go)",
			"strace -o out ls":                        "strace(ls)",
			"nsenter -t 1 ip a":                       "nsenter(ip)",
		}
		for command, want := range cases {
			if got := programs(command); got != want {
				t.Errorf("%q: %q, want %q", command, got, want)
			}
		}
	})

	t.Run("LT-10 once a call ran, the state follows the model again, reasoning or talking, not the call", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m, llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "w1", Name: "web_search", Text: `{"query":"tv off"}`})
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "w1", Name: "web_search"}})
		ended, _ := started.(model).Update(streamEndMsg{})
		done, _ := ended.(model).Update(toolDoneMsg{id: "w1", output: "results"})
		thinking := streaming(done.(model), llm.Event{Kind: llm.EventReasoning, Text: "nothing there"})

		if got := thinking.currentState(); got.word != "reasoning" {
			t.Errorf("state %q · %q", got.word, got.detail)
		}
	})

	t.Run("LT-11 while the model writes a call the state is that call; once done, it waits for the tools", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m,
			llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "read", Text: `{"path":"a.go"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "edit", Text: `{"path":"b.go"`})
		writing := m.currentState()
		ended, _ := m.Update(streamEndMsg{})
		done := ended.(model).currentState()

		if writing.word != "editing" || done.word != "waiting" || done.detail != "for the tools" {
			t.Errorf("writing %q · %q, done %q · %q", writing.word, writing.detail, done.word, done.detail)
		}
	})

	t.Run("LT-12 a running call is what the state says, and the calls behind it do not change that", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}
		m = streaming(m,
			llm.Event{Kind: llm.EventToolCall, Call: 0, ID: "c1", Name: "bash", Text: `{"command":"make"}`},
			llm.Event{Kind: llm.EventToolCall, Call: 1, ID: "c2", Name: "edit", Text: `{"path":"b.go"}`})
		started, _ := m.Update(toolStartMsg{call: llm.ToolCall{ID: "c1", Name: "bash", Args: []byte(`{"command":"make"}`)}})
		ended, _ := started.(model).Update(streamEndMsg{})
		running := ended.(model).currentState()
		finished, _ := ended.(model).Update(toolDoneMsg{id: "c1", output: "ok"})
		after := finished.(model).currentState()

		if running.word != "running" || after.word != "waiting" {
			t.Errorf("running %q · %q, after %q · %q", running.word, running.detail, after.word, after.detail)
		}
	})

	t.Run("LT-07 while llama-server reads the prompt, the state says how far, cache left out", func(t *testing.T) {
		m := conversationModel()
		m.running, m.cancel = true, func() {}

		reading := streaming(m, llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 5000, Total: 9000, Cached: 1000}}).currentState()
		answering := streaming(streaming(m, llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 9000, Total: 9000}}), llm.Event{Kind: llm.EventText, Text: "Hi"}).currentState()

		if reading.detail != "50% · 4.0k/8.0k · 1.0k cached" || answering.word != "talking" {
			t.Errorf("reading %q answering %q", reading.detail, answering.word)
		}
	})
}

// breathing draws every breath of a call being written as its first.
func breathing(text string) string {
	for _, frame := range formingFrames {
		text = strings.ReplaceAll(text, frame+" ", formingFrames[0]+" ")
	}
	return text
}
