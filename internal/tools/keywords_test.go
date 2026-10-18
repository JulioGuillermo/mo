package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | KW-01 | Happy    | A query in the words a model would use finds the tool among the first    | P0       |
// | KW-02 | Happy    | Tools of one topic share its words: file, folder, internet               | P0       |
// | KW-03 | Security | The keywords never reach the model, not in the specs nor in a search     | P0       |
// | KW-04 | Edge     | A tool whose description was emptied is still found by its keywords      | P1       |
// | KW-05 | Edge     | A word in a tool's name counts more than one in its keywords             | P1       |
// | KW-06 | Happy    | Words in Spanish find the tools too                                      | P2       |
// | KW-07 | Edge     | A query of nothing the tools say finds none                              | P2       |
// | KW-08 | Edge     | The tool that has more of the query's words comes first                  | P0       |
// | KW-09 | Edge     | A word that is a keyword counts more than one found inside a keyword     | P2       |

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func keywordRegistry(t *testing.T, options Options) *Registry {
	t.Helper()
	options.Mode = ModeSearch
	return NewRegistry(newEnv(t), options)
}

func foundNames(registry *Registry, query string) []string {
	var names []string
	for _, tool := range registry.match(query) {
		names = append(names, tool.Name)
	}
	return names
}

func TestKeywords(t *testing.T) {
	t.Run("KW-01 a query in the words a model would use finds the tool among the first", func(t *testing.T) {
		registry := keywordRegistry(t, Options{})
		cases := map[string]string{
			"delete a file":           "rm",
			"remove directory":        "rm",
			"erase":                   "rm",
			"change a file":           "edit",
			"replace text in file":    "edit",
			"create new file":         "write",
			"save to disk":            "write",
			"download a page":         "web_fetch",
			"get url":                 "web_fetch",
			"google something":        "web_search",
			"latest news":             "web_search",
			"find text in files":      "grep",
			"regex":                   "grep",
			"show folder contents":    "ls",
			"make folder":             "mkdir",
			"run a command":           "bash",
			"compile and test":        "bash",
			"open an image":           "read",
			"remember this":           "memory",
			"notes from past chats":   "memory",
			"checklist of tasks":      "todo",
			"ask the user a question": "ask",
			"commit changes":          "git",
			"repository branch":       "git",
			"rename the conversation": "title",
		}
		for query, want := range cases {
			names := foundNames(registry, query)
			if at := slices.Index(names, want); at < 0 || at > 2 {
				t.Errorf("%q found %v, want %s among the first 3", query, names, want)
			}
		}
	})

	t.Run("KW-02 tools of one topic share its words: file, folder, internet", func(t *testing.T) {
		registry := keywordRegistry(t, Options{})
		topics := map[string][]string{
			"file":     {"read", "write", "edit", "ls", "rm", "grep"},
			"folder":   {"ls", "mkdir", "rm"},
			"internet": {"web_fetch", "web_search"},
		}
		for word, want := range topics {
			listed := registry.list(word)
			for _, name := range want {
				if !strings.Contains(listed, name) {
					t.Errorf("tool_list %q does not name %s: %s", word, name, listed)
				}
			}
		}
	})

	t.Run("KW-03 the keywords never reach the model, not in the specs nor in a search", func(t *testing.T) {
		registry := keywordRegistry(t, Options{AlwaysLoaded: []string{"read"}})
		var sent strings.Builder
		for _, spec := range registry.Specs(nil) {
			sent.WriteString(spec.Name + " " + spec.Description + " " + string(spec.Schema) + " ")
		}
		sent.WriteString(registry.search("delete a file"))
		sent.WriteString(registry.list("delete"))

		for _, word := range []string{"unlink", "ripgrep", "wipe", "duckduckgo"} {
			if strings.Contains(strings.ToLower(sent.String()), word) {
				t.Errorf("%q reached the model", word)
			}
		}
	})

	t.Run("KW-04 a tool whose description was emptied is still found by its keywords", func(t *testing.T) {
		registry := keywordRegistry(t, Options{Descriptions: map[string]string{"rm": ""}})

		names := foundNames(registry, "delete")

		if !slices.Contains(names, "rm") {
			t.Errorf("found %v", names)
		}
	})

	t.Run("KW-05 a word in a tool's name counts more than one in its keywords", func(t *testing.T) {
		registry := keywordRegistry(t, Options{})

		for query, want := range map[string]string{"read": "read", "grep": "grep", "fetch": "web_fetch", "web_fetch": "web_fetch", "memory": "memory", "git": "git"} {
			if names := foundNames(registry, query); len(names) == 0 || names[0] != want {
				t.Errorf("%q found %v", query, names)
			}
		}
	})

	t.Run("KW-06 words in Spanish find the tools too", func(t *testing.T) {
		registry := keywordRegistry(t, Options{})
		cases := map[string]string{
			"borrar archivo":      "rm",
			"buscar en internet":  "web_search",
			"crear carpeta":       "mkdir",
			"leer archivo":        "read",
			"ejecutar un comando": "bash",
			"recordar":            "memory",
		}
		for query, want := range cases {
			if names := foundNames(registry, query); !slices.Contains(names[:min(len(names), 3)], want) {
				t.Errorf("%q found %v, want %s among the first 3", query, names, want)
			}
		}
	})

	t.Run("KW-07 a query of nothing the tools say finds none", func(t *testing.T) {
		registry := keywordRegistry(t, Options{})

		if names := foundNames(registry, "zzzqqq xyzzy"); len(names) != 0 {
			t.Errorf("found %v", names)
		}
	})

	t.Run("KW-08 the tool that has more of the query's words comes first", func(t *testing.T) {
		registry := keywordRegistry(t, Options{})
		cases := map[string]string{
			"search the web":    "web_search",
			"delete file":       "rm",
			"find text in file": "grep",
			"read image file":   "read",
		}
		for query, want := range cases {
			if names := foundNames(registry, query); len(names) == 0 || names[0] != want {
				t.Errorf("%q found %v, want %s first", query, names, want)
			}
		}
	})

	t.Run("KW-09 a word that is a keyword counts more than one found inside a keyword", func(t *testing.T) {
		extra := func(name string, keywords ...string) Tool {
			return Tool{Name: name, Description: "Does something.", Schema: json.RawMessage(`{"type":"object"}`), Keywords: keywords}
		}
		registry := keywordRegistry(t, Options{Extra: []Tool{extra("inside", "deployment"), extra("exact", "deploy")}})

		names := foundNames(registry, "deploy")

		if len(names) < 2 || names[0] != "exact" || names[1] != "inside" {
			t.Errorf("found %v", names)
		}
	})
}
