package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jg/mo/internal/gitops"
)

// gitChanges are the actions that change the repository: they ask like
// file changes do; the others only read.
var gitChanges = []string{"add", "commit", "branch", "switch", "merge", "cherry_pick", "rebase", "continue", "abort", "pull", "push",
	"stash", "stash_apply", "stash_pop", "stash_drop", "tag", "reset"}

var gitHelp = []helpTopic{
	{"status", "branch, what is staged, changed and untracked."},
	{"diff", "changes not staged; staged: true, those staged. paths: only these."},
	{"log", "commits, the latest first. count: how many (20). path: only those touching it."},
	{"show", "revision: a commit with its diff (HEAD)."},
	{"blame", "path: who last changed each line."},
	{"branches", "local and remote branches, the current one marked."},
	{"check", "the files with conflict markers left."},
	{"fetch", "remote: get its branches and tags (origin)."},
	{"stash_list", "the stashes, 0 the newest."},
	{"tags", "the tags."},
	{"add", "paths: stage them; none: stage everything."},
	{"commit", "message: commit what is staged."},
	{"branch", "name: create a branch at HEAD."},
	{"switch", "name: switch to a branch; create: true makes it first."},
	{"merge", "revision: merge it into the current branch."},
	{"cherry_pick", "revision: apply that commit here."},
	{"rebase", "revision: replay the current branch onto it."},
	{"continue", "go on with a merge, cherry_pick or rebase once its conflicts are fixed."},
	{"abort", "give up a merge, cherry_pick or rebase."},
	{"pull", "remote, name: the branch to take (the one tracked); rebase: true replays yours on top, false merges, unset as git's pull.rebase says."},
	{"push", "remote: push the current branch (origin); never forced."},
	{"stash", "message: put the tracked changes aside; untracked files stay."},
	{"stash_apply", "index: apply a stash and keep it (0, the newest)."},
	{"stash_pop", "index: apply a stash and drop it."},
	{"stash_drop", "index: drop a stash for good."},
	{"tag", "name, revision (HEAD), message: annotated when given."},
	{"reset", "revision, mode: soft, mixed (default) or hard; untracked files stay."},
	{"conflicts", "they stay as markers in the files: edit them away, then continue; commit and continue refuse while markers are left."},
}

func gitTool() Tool {
	return Tool{
		Name: "git",
		Description: "Git in the working directory, its output kept short; hooks do not run. " +
			`{"action":"help"} explains each action; for what they do not do, run git with bash.`,
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["status","diff","log","show","blame","branches","check","fetch","stash_list","tags","add","commit","branch","switch","merge","cherry_pick","rebase","continue","abort","pull","push","stash","stash_apply","stash_pop","stash_drop","tag","reset","help"]},
"paths":{"type":"array","items":{"type":"string"}},"path":{"type":"string"},"revision":{"type":"string"},"name":{"type":"string"},"message":{"type":"string"},
"remote":{"type":"string"},"staged":{"type":"boolean"},"create":{"type":"boolean"},"rebase":{"type":"boolean"},"mode":{"type":"string","enum":["soft","mixed","hard"]},
"index":{"type":"integer"},"count":{"type":"integer"},"topic":{"type":"string"}},"required":["action"]}`),
		Run:  runGit,
		View: true,
		Summary: func(args json.RawMessage) string {
			parts := []string{argField(args, "action")}
			for _, field := range []string{"revision", "name", "path", "message", "topic"} {
				if value := argField(args, field); value != "" {
					parts = append(parts, value)
				}
			}
			return firstLineOf(strings.Join(parts, " "))
		},
		Changes: func(args json.RawMessage) bool { return slices.Contains(gitChanges, argField(args, "action")) },
		Warning: func(args json.RawMessage) string {
			switch {
			case argField(args, "action") == "push":
				return "publishes to " + firstNonEmpty(argField(args, "remote"), "origin")
			case argField(args, "action") == "reset" && argField(args, "mode") == "hard":
				return "drops the changes not committed"
			case argField(args, "action") == "stash_drop":
				return "drops a stash for good"
			}
			return ""
		},
	}
}

func runGit(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Action, Path, Revision, Message, Name, Remote, Mode, Topic string
		Index, Count                                               int
		Paths                                                      []string
		Create, Staged                                             bool
		Rebase                                                     *bool
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Action == "help" {
		return help(gitHelp, args.Topic)
	}
	repo, err := gitops.Open(env.WorkDir)
	if err != nil {
		return "", err
	}
	switch args.Action {
	case "status":
		return repo.Status()
	case "diff":
		return repo.Diff(args.Staged, args.Paths)
	case "log":
		return repo.Log(args.Count, args.Path)
	case "show":
		return repo.Show(args.Revision)
	case "blame":
		return repo.Blame(args.Path)
	case "branches":
		return repo.Branches()
	case "check":
		return repo.Check()
	case "add":
		return repo.Add(args.Paths)
	case "commit":
		return repo.Commit(args.Message)
	case "branch":
		return repo.Branch(args.Name)
	case "switch":
		return repo.Switch(args.Name, args.Create)
	case "merge":
		return repo.Merge(args.Revision)
	case "cherry_pick":
		return repo.CherryPick(args.Revision)
	case "rebase":
		return repo.Rebase(args.Revision)
	case "fetch":
		return repo.Fetch(args.Remote)
	case "pull":
		return repo.Pull(gitops.PullOptions{Remote: args.Remote, Branch: args.Name, Rebase: args.Rebase})
	case "push":
		return repo.Push(args.Remote)
	case "stash":
		return repo.Stash(args.Message)
	case "stash_list":
		return repo.Stashes()
	case "stash_apply":
		return repo.ApplyStash(args.Index, false)
	case "stash_pop":
		return repo.ApplyStash(args.Index, true)
	case "stash_drop":
		return repo.DropStash(args.Index)
	case "tags":
		return repo.Tags()
	case "tag":
		return repo.Tag(args.Name, args.Revision, args.Message)
	case "reset":
		return repo.Reset(args.Revision, args.Mode)
	case "continue":
		return repo.Continue()
	case "abort":
		return repo.Abort()
	}
	return "", fmt.Errorf("action %q is not one git takes", args.Action)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
