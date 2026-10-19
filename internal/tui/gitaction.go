package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tools"
)

// gitAction is one /git action: how it is typed and what it does.
type gitAction struct {
	name, usage, description string
	// needsMore is set when the action takes a word it cannot do without,
	// so a suggestion fills it in instead of running it.
	needsMore bool
}

var gitActions = []gitAction{
	{"show", "[repo] [revision]", "the changes not committed in the viewer; with a revision, that commit's", false},
	{"compare", "[repo] <branch>", "what this branch adds over another, as a pull request", true},
	{"branch", "[repo] [name]", "the branches, to filter and switch; with a name, make that branch", false},
	{"repos", "", "the repositories known, each with its branch", false},
	{"status", "[repo]", "the branch, what is staged, changed and untracked", false},
	{"diff", "[repo] [--staged] [paths]", "the changes not staged, or the staged ones, as text", false},
	{"log", "[repo] [-n N] [path]", "the commits, the latest first (20)", false},
	{"blame", "[repo] <path>", "who last changed each line", true},
	{"check", "[repo]", "what is half done and the files with conflict markers", false},
	{"switch", "[repo] [-c] <branch>", "switch to a branch; -c makes it first", true},
	{"add", "[repo] [paths]", "stage the paths, or every change", false},
	{"commit", "[repo] <message>", "commit what is staged", true},
	{"fetch", "[repo] [remote]", "get the remote's branches and tags (origin)", false},
	{"pull", "[repo] [remote] [branch] [--rebase|--merge]", "bring the tracked branch in", false},
	{"push", "[repo] [remote]", "push the current branch (origin); never forced", false},
	{"merge", "[repo] <revision>", "merge it into the current branch", true},
	{"rebase", "[repo] <revision>", "replay the current branch onto it", true},
	{"cherry-pick", "[repo] <revision>", "apply that commit here", true},
	{"continue", "[repo]", "go on with a merge, cherry-pick or rebase once its conflicts are fixed", false},
	{"abort", "[repo]", "give up a merge, cherry-pick or rebase", false},
	{"stash", "[repo] [message] | list | apply|pop|drop [n]", "put the tracked changes aside, list them, bring one back or drop it", false},
	{"tags", "[repo]", "the tags", false},
	{"tag", "[repo] <name> [revision] [-m message]", "make a tag, annotated with a message", true},
	{"reset", "[repo] [revision] [--soft|--mixed|--hard]", "move the branch (HEAD); --hard drops the changes not committed", false},
	{"init", "[folder]", "make a repository, the folder too when missing", false},
}

func gitUsage() string {
	lines := []string{"/git <action>, the repository after it when it is not this folder's:"}
	for _, action := range gitActions {
		lines = append(lines, fmt.Sprintf("  %s %s — %s", action.name, action.usage, action.description))
	}
	return strings.Join(lines, "\n")
}

// gitDoneMsg brings what a /git action gave.
type gitDoneMsg struct {
	id      string
	output  string
	failed  bool
	changed bool
}

// runGitAction runs an action of the git tool for the user, off the UI,
// shown as a call of it; root is the repository, "" for init.
func (m model) runGitAction(request gitRequest, root string) (tea.Model, tea.Cmd) {
	args, err := gitToolArgs(request)
	if err != nil {
		return m.emit(m.printer.Note(err.Error()))
	}
	if root != "" {
		args["repo"] = root
	} else if request.repo != "" {
		args["repo"] = m.absolutePath(request.repo)
	}
	action, _ := args["action"].(string)
	changes := tools.GitChanges(action)
	if changes && m.running {
		return m.emit(m.printer.Note("the model is working: /git " + request.action + " changes the repository, run it once it ends"))
	}
	raw, _ := json.Marshal(args)
	m.userRuns++
	id := fmt.Sprintf("user-git-%d", m.userRuns)
	summary := strings.TrimSpace(request.action + " " + request.text)
	if request.repo != "" {
		summary = strings.TrimSpace(request.action + " " + request.repo + " " + request.text)
	}
	m.addTool(llm.ToolCall{ID: id, Name: "git", Args: raw}, summary)
	workDir := m.session.WorkDir
	run := func() tea.Msg {
		output, err := tools.RunGit(context.Background(), workDir, raw)
		if err != nil {
			return gitDoneMsg{id: id, output: strings.TrimSpace(output + "\n" + err.Error()), failed: true}
		}
		return gitDoneMsg{id: id, output: output, changed: changes}
	}
	return m, run
}

func (m model) finishGitAction(msg gitDoneMsg) (tea.Model, tea.Cmd) {
	m.finishTool(msg.id, msg.output, msg.failed)
	return m, m.readRepo()
}

// gitToolArgs turns what was typed after a /git action into the git
// tool's arguments.
func gitToolArgs(request gitRequest) (map[string]any, error) {
	words := request.words
	usage := func() error {
		for _, action := range gitActions {
			if action.name == request.action {
				return fmt.Errorf("usage: /git %s %s", action.name, action.usage)
			}
		}
		return fmt.Errorf("/git has no action %q; /git help lists them", request.action)
	}
	switch request.action {
	case "status", "check", "continue", "abort", "tags", "init":
		if len(words) > 0 {
			return nil, usage()
		}
		return map[string]any{"action": request.action}, nil
	case "diff":
		args := map[string]any{"action": "diff"}
		var paths []string
		for _, word := range words {
			if word == "--staged" || word == "--cached" {
				args["staged"] = true
				continue
			}
			paths = append(paths, word)
		}
		if len(paths) > 0 {
			args["paths"] = paths
		}
		return args, nil
	case "log":
		return logArgs(words, usage)
	case "blame":
		if len(words) != 1 {
			return nil, usage()
		}
		return map[string]any{"action": "blame", "path": words[0]}, nil
	case "branch":
		if len(words) != 1 {
			return nil, usage()
		}
		return map[string]any{"action": "branch", "name": words[0]}, nil
	case "switch":
		create := len(words) == 2 && words[0] == "-c"
		if create {
			words = words[1:]
		}
		if len(words) != 1 {
			return nil, usage()
		}
		return map[string]any{"action": "switch", "name": words[0], "create": create}, nil
	case "add":
		args := map[string]any{"action": "add"}
		if len(words) > 0 {
			args["paths"] = words
		}
		return args, nil
	case "commit":
		message := typedMessage(request.text)
		if message == "" {
			return nil, usage()
		}
		return map[string]any{"action": "commit", "message": message}, nil
	case "fetch", "push":
		return remoteArgs(request.action, words, usage)
	case "pull":
		return pullArgs(words, usage)
	case "merge", "rebase", "cherry-pick":
		if len(words) != 1 {
			return nil, usage()
		}
		return map[string]any{"action": strings.ReplaceAll(request.action, "-", "_"), "revision": words[0]}, nil
	case "stash":
		return stashArgs(request, usage)
	case "tag":
		return tagArgs(request.text, usage)
	case "reset":
		return resetArgs(words, usage)
	}
	return nil, usage()
}

func logArgs(words []string, usage func() error) (map[string]any, error) {
	args := map[string]any{"action": "log"}
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case word == "-n" && i+1 < len(words):
			count, err := strconv.Atoi(words[i+1])
			if err != nil {
				return nil, usage()
			}
			args["count"] = count
			i++
		case strings.HasPrefix(word, "-"):
			count, err := strconv.Atoi(word[1:])
			if err != nil {
				return nil, usage()
			}
			args["count"] = count
		case args["path"] == nil:
			args["path"] = word
		default:
			return nil, usage()
		}
	}
	return args, nil
}

func remoteArgs(action string, words []string, usage func() error) (map[string]any, error) {
	if len(words) > 1 {
		return nil, usage()
	}
	args := map[string]any{"action": action}
	if len(words) == 1 {
		args["remote"] = words[0]
	}
	return args, nil
}

func pullArgs(words []string, usage func() error) (map[string]any, error) {
	args := map[string]any{"action": "pull"}
	var named []string
	for _, word := range words {
		switch word {
		case "--rebase":
			args["rebase"] = true
		case "--merge", "--no-rebase":
			args["rebase"] = false
		default:
			named = append(named, word)
		}
	}
	if len(named) > 2 {
		return nil, usage()
	}
	if len(named) > 0 {
		args["remote"] = named[0]
	}
	if len(named) > 1 {
		args["name"] = named[1]
	}
	return args, nil
}

func stashArgs(request gitRequest, usage func() error) (map[string]any, error) {
	words := request.words
	if len(words) == 0 {
		return map[string]any{"action": "stash"}, nil
	}
	switch words[0] {
	case "list":
		if len(words) > 1 {
			return nil, usage()
		}
		return map[string]any{"action": "stash_list"}, nil
	case "apply", "pop", "drop":
		args := map[string]any{"action": "stash_" + words[0]}
		if len(words) > 2 {
			return nil, usage()
		}
		if len(words) == 2 {
			index, err := strconv.Atoi(strings.Trim(words[1], "stash@{}"))
			if err != nil {
				return nil, usage()
			}
			args["index"] = index
		}
		return args, nil
	}
	return map[string]any{"action": "stash", "message": typedMessage(request.text)}, nil
}

func tagArgs(text string, usage func() error) (map[string]any, error) {
	before, message, annotated := strings.Cut(text, "-m ")
	words := strings.Fields(before)
	if len(words) == 0 || len(words) > 2 || (annotated && typedMessage(message) == "") {
		return nil, usage()
	}
	args := map[string]any{"action": "tag", "name": words[0]}
	if len(words) == 2 {
		args["revision"] = words[1]
	}
	if annotated {
		args["message"] = typedMessage(message)
	}
	return args, nil
}

func resetArgs(words []string, usage func() error) (map[string]any, error) {
	args := map[string]any{"action": "reset"}
	for _, word := range words {
		switch {
		case word == "--soft" || word == "--mixed" || word == "--hard":
			args["mode"] = strings.TrimPrefix(word, "--")
		case args["revision"] == nil && !strings.HasPrefix(word, "-"):
			args["revision"] = word
		default:
			return nil, usage()
		}
	}
	return args, nil
}

// typedMessage is a message as typed after a command: a leading -m and
// the quotes around it dropped.
func typedMessage(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSpace(strings.TrimPrefix(text, "-m "))
	if len(text) >= 2 && (text[0] == '"' || text[0] == '\'') && text[len(text)-1] == text[0] {
		text = text[1 : len(text)-1]
	}
	return text
}
