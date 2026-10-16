package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	projectHashBytes   = 4
	memoryScopeProject = "project"
	memoryScopeGlobal  = "global"
)

var memoryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var memoryHelp = []helpTopic{
	{"list", "the notes of this project and the global ones, each with its summary, the first line."},
	{"read", "name: a note, or an id search gave: m:name a note, c:id a past conversation's turns, c:id#3 one turn whole."},
	{"write", "name (lowercase letters, digits, - and _), content: save a note; scope: project (default) or global, for every project."},
	{"delete", "name, scope: delete a note."},
	{"search", "query: words to find in the notes and past conversations; scope: project (default) or all, every project's. kind: notes or conversations only. count: results (6, at most 20)."},
}

func memoryTool() Tool {
	return Tool{
		Name:        "memory",
		Description: "Notes kept across conversations, and search over them and past conversations. " + `{"action":"help"} explains each action.`,
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["list","read","write","delete","search","help"]},
"name":{"type":"string"},"content":{"type":"string"},"query":{"type":"string"},
"scope":{"type":"string","enum":["project","global","all"]},"kind":{"type":"string","enum":["notes","conversations"]},
"count":{"type":"integer"},"topic":{"type":"string"}},"required":["action"]}`),
		Run:  runMemory,
		View: true,
		Summary: func(args json.RawMessage) string {
			return strings.TrimSpace(argField(args, "action") + " " + argField(args, "name") + argField(args, "query") + argField(args, "topic"))
		},
	}
}

func runMemory(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Action, Name, Content, Query, Scope, Kind, Topic string
		Count                                            int
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	switch args.Action {
	case "help":
		return help(memoryHelp, args.Topic)
	case "search":
		if env.Recall == nil {
			return "", errors.New("search is not available here")
		}
		count := args.Count
		if count <= 0 {
			count = recallResults
		}
		kind := map[string]string{"notes": kindMemories, "conversations": kindConversations}[args.Kind]
		scope := ""
		if args.Scope == "all" {
			scope = scopeAll
		}
		return env.Recall.search(env, args.Query, scope, kind, min(count, mostRecallResults))
	}
	if env.MemoryDir == "" {
		return "", errors.New("memory is not configured")
	}
	if args.Action == "list" {
		return memoryList(env), nil
	}
	if args.Action == "read" && strings.Contains(args.Name, ":") {
		if env.Recall == nil {
			return readMemory(env, strings.TrimPrefix(args.Name, memoryPrefix))
		}
		return env.Recall.read(env, args.Name)
	}
	if args.Scope == "all" {
		return "", errors.New("scope all is for search: use project or global")
	}
	path, err := memoryPath(env, args.Scope, args.Name)
	if err != nil {
		return "", err
	}
	switch args.Action {
	case "read":
		data, err := os.ReadFile(path)
		return string(data), err
	case "write":
		if strings.TrimSpace(args.Content) == "" {
			return "", errors.New("content is empty")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(args.Content), 0o600); err != nil {
			return "", err
		}
		return "saved", nil
	case "delete":
		if err := os.Remove(path); err != nil {
			return "", err
		}
		return "deleted", nil
	}
	return "", fmt.Errorf("action %q: use list, read, write, delete, search or help", args.Action)
}

func memoryPath(env Env, scope, name string) (string, error) {
	if !memoryNamePattern.MatchString(name) {
		return "", errors.New("name must be lowercase letters, digits, - or _ (max 64)")
	}
	dir, err := memoryScopeDir(env, scope)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".md"), nil
}

func memoryScopeDir(env Env, scope string) (string, error) {
	switch scope {
	case "", memoryScopeProject:
		return filepath.Join(env.MemoryDir, "projects", projectSlug(env.WorkDir)), nil
	case memoryScopeGlobal:
		return filepath.Join(env.MemoryDir, memoryScopeGlobal), nil
	}
	return "", fmt.Errorf("unknown scope %q", scope)
}

// projectSlug names a project's memory folder after the last part of its
// path plus a hash of the whole path: /home/jg/mo becomes mo-1f2e3d4c.
func projectSlug(workDir string) string {
	clean := filepath.Clean(workDir)
	hash := sha256.Sum256([]byte(clean))
	return fmt.Sprintf("%s-%x", filepath.Base(clean), hash[:projectHashBytes])
}

func memoryList(env Env) string {
	entries := memoryEntries(env)
	if len(entries) == 0 {
		return "no memories"
	}
	return strings.Join(entries, "\n")
}

func memoryEntries(env Env) []string {
	if env.MemoryDir == "" {
		return nil
	}
	var lines []string
	for _, scope := range []string{memoryScopeProject, memoryScopeGlobal} {
		dir, _ := memoryScopeDir(env, scope)
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			name, isMemory := strings.CutSuffix(entry.Name(), ".md")
			if !isMemory || !entry.Type().IsRegular() {
				continue
			}
			summary := memorySummary(filepath.Join(dir, entry.Name()))
			lines = append(lines, fmt.Sprintf("%s/%s: %s", scope, name, summary))
		}
	}
	return lines
}

func memorySummary(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return cutAtRune(firstLineOf(string(data)), 120)
}

func firstLineOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(strings.TrimLeft(line, "# "))
}
