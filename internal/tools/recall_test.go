package tools

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | RC-01 | Happy    | search finds past turns by their words, the most relevant first, with a piece around the match | P0 |
// | RC-02 | Happy    | Memories are found too; kind keeps to one or the other; read gives a memory whole | P0 |
// | RC-03 | Happy    | Scope here keeps to this folder, all takes every one; the conversation in progress is left out | P0 |
// | RC-04 | Happy    | read lists a conversation's turns, or gives one whole with its tool results | P0 |
// | RC-05 | Security | An id with a path is refused                                         | P0       |
// | RC-06 | Edge     | A conversation saved again is read again                             | P1       |

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

type recallSetup struct {
	registry *Registry
	recall   *Recall
	dir      string
	env      Env
}

func newRecallSetup(t *testing.T) recallSetup {
	t.Helper()
	sessionDir := t.TempDir()
	recall := NewRecall(sessionDir)
	env := Env{WorkDir: t.TempDir(), MemoryDir: t.TempDir(), Recall: recall}
	return recallSetup{registry: NewRegistry(env, Options{MaxOutput: 20000}), recall: recall, dir: sessionDir, env: env}
}

func (s recallSetup) save(t *testing.T, id, workDir, title string, when time.Time, history ...llm.Message) {
	t.Helper()
	if err := sessions.Save(s.dir, sessions.Record{Info: sessions.Info{ID: id, WorkDir: workDirKey(workDir), Title: title, Updated: when}, History: history}); err != nil {
		t.Fatal(err)
	}
}

// run searches or reads through memory, taking the fields recall took: id
// is the name read, kind memories is notes, scope here is the default and
// limit the count.
func (s recallSetup) run(t *testing.T, fields map[string]any) (string, bool) {
	t.Helper()
	renamed := map[string]any{}
	for key, value := range fields {
		switch {
		case key == "id":
			renamed["name"] = value
		case key == "kind" && value == "memories":
			renamed["kind"] = "notes"
		case key == "scope" && value == "here":
		case key == "limit":
			renamed["count"] = value
		default:
			renamed[key] = value
		}
	}
	return s.registry.Run(context.Background(), llm.ToolCall{Name: "memory", Args: args(t, renamed)})
}

func user(text string) llm.Message      { return llm.Message{Role: llm.RoleUser, Text: text} }
func assistant(text string) llm.Message { return llm.Message{Role: llm.RoleAssistant, Text: text} }

var day = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestRecall(t *testing.T) {
	t.Run("RC-01 search finds past turns by their words, the most relevant first, with a piece around the match", func(t *testing.T) {
		s := newRecallSetup(t)
		s.save(t, "deploy", s.env.WorkDir, "deploy the api", day,
			user("how do we deploy the api"), assistant("With kubectl apply -f deploy.yaml after the build; kubectl rollout status waits for it."),
			user("and the database?"), assistant("Migrations run first with make migrate."))
		s.save(t, "lunch", s.env.WorkDir, "lunch", day.Add(time.Hour), user("what is for lunch"), assistant("Soup, then kubectl jokes."))

		output, isError := s.run(t, map[string]any{"action": "search", "query": "kubectl rollout"})

		lines := strings.Split(output, "\n")
		if isError || !strings.HasPrefix(lines[0], "c:deploy#1 · \"deploy the api\"") || !strings.Contains(lines[1], "kubectl rollout status") || !strings.Contains(output, "c:lunch#1") {
			t.Errorf("output:\n%s", output)
		}
		if strings.Contains(output, "c:deploy#2") {
			t.Errorf("a turn without the words was found:\n%s", output)
		}
	})

	t.Run("RC-02 memories are found too; kind keeps to one or the other; read gives a memory whole", func(t *testing.T) {
		s := newRecallSetup(t)
		memory(t, s.env, map[string]any{"action": "write", "name": "vpn", "content": "VPN setup\nwireguard on port 51820, keys in vault"})
		memory(t, s.env, map[string]any{"action": "write", "name": "me", "content": "Uses wireguard daily", "scope": "global"})
		s.save(t, "net", s.env.WorkDir, "network", day, user("wireguard is down"), assistant("restart it"))

		both, _ := s.run(t, map[string]any{"action": "search", "query": "wireguard"})
		memories, _ := s.run(t, map[string]any{"action": "search", "query": "wireguard", "kind": "memories"})
		whole, isError := s.run(t, map[string]any{"action": "read", "id": "m:project/vpn"})

		if !strings.Contains(both, "m:project/vpn") || !strings.Contains(both, "m:global/me") || !strings.Contains(both, "c:net#1") || strings.Contains(memories, "c:net") {
			t.Errorf("both:\n%s\nmemories:\n%s", both, memories)
		}
		if isError || whole != "VPN setup\nwireguard on port 51820, keys in vault\n" {
			t.Errorf("whole = %q", whole)
		}
	})

	t.Run("RC-03 scope here keeps to this folder, all takes every one; the conversation in progress is left out", func(t *testing.T) {
		s := newRecallSetup(t)
		s.save(t, "here", s.env.WorkDir, "here", day, user("the parser panics"), assistant("fixed"))
		s.save(t, "elsewhere", t.TempDir(), "elsewhere", day, user("the parser is slow"), assistant("cached"))
		s.save(t, "now", s.env.WorkDir, "now", day, user("parser again"), assistant("looking"))
		s.recall.SetCurrent("now")

		here, _ := s.run(t, map[string]any{"action": "search", "query": "parser"})
		all, _ := s.run(t, map[string]any{"action": "search", "query": "parser", "scope": "all"})

		if !strings.Contains(here, "c:here#1") || strings.Contains(here, "elsewhere") || strings.Contains(here, "c:now") || !strings.Contains(all, "c:elsewhere#1") {
			t.Errorf("here:\n%s\nall:\n%s", here, all)
		}
	})

	t.Run("RC-04 read lists a conversation's turns, or gives one whole with its tool results", func(t *testing.T) {
		s := newRecallSetup(t)
		long := strings.Repeat("line of output\n", 50)
		s.save(t, "build", s.env.WorkDir, "fix the build", day,
			user("fix the build"),
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "bash", Args: []byte(`{"command":"go build ./..."}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "1", Text: long},
			assistant("Built."), user("now the tests"), assistant("They pass."))

		turns, _ := s.run(t, map[string]any{"action": "read", "id": "c:build"})
		first, _ := s.run(t, map[string]any{"action": "read", "id": "c:build#1"})
		_, beyond := s.run(t, map[string]any{"action": "read", "id": "c:build#3"})

		if !strings.Contains(turns, "2 turns") || !strings.Contains(turns, "c:build#2: now the tests") {
			t.Errorf("turns:\n%s", turns)
		}
		if !strings.Contains(first, "turn 1 of 2") || !strings.Contains(first, `[bash {"command":"go build ./..."}]`) || strings.Count(first, "line of output") < 50 || !strings.Contains(first, "Built.") {
			t.Errorf("first:\n%s", first)
		}
		if !beyond {
			t.Error("a turn past the end was read")
		}
	})

	t.Run("RC-05 an id with a path is refused", func(t *testing.T) {
		s := newRecallSetup(t)
		secret := filepath.Join(s.env.MemoryDir, "secret.md")
		_ = os.WriteFile(secret, []byte("x"), 0o600)

		for _, id := range []string{"m:../secret", "m:../../etc/passwd", "m:./x", "c:../sessions", "c:/etc/passwd"} {
			if output, isError := s.run(t, map[string]any{"action": "read", "id": id}); !isError {
				t.Errorf("%s was read: %q", id, output)
			}
		}
	})

	t.Run("RC-06 a conversation saved again is read again", func(t *testing.T) {
		s := newRecallSetup(t)
		s.save(t, "grow", s.env.WorkDir, "grow", day, user("first idea"), assistant("ok"))
		before, _ := s.run(t, map[string]any{"action": "search", "query": "zeppelin"})
		time.Sleep(20 * time.Millisecond)
		s.save(t, "grow", s.env.WorkDir, "grow", day, user("first idea"), assistant("ok"), user("zeppelin plans"), assistant("noted"))

		after, _ := s.run(t, map[string]any{"action": "search", "query": "zeppelin"})

		if !strings.Contains(before, "nothing found") || !strings.Contains(after, "c:grow#2") {
			t.Errorf("before = %q after = %q", before, after)
		}
	})
}
