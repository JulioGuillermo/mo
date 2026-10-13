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

func gitTool() Tool {
	return Tool{
		Name: "git",
		Description: "Git in the working directory, without the git command. Read: status, diff (staged: the index), log (path, limit), show (revision), blame (path), branches, " +
			"check (conflicts left), fetch (remote), stash_list, tags. Change: add (paths; none: all), commit (message), branch (name), switch (branch, create), merge (revision), cherry_pick (revision), " +
			"rebase (revision to replay onto), continue, abort, " +
			"pull (remote, branch: theirs to take, default the one tracked; rebase: true replays yours on top, false merges, unset as git's pull.rebase says), push (remote; never forced), " +
			"stash (message), stash_apply/stash_pop/stash_drop (index, 0 newest), tag (name, revision, message: annotated), reset (revision, mode soft|mixed|hard). " +
			"Untracked files are never touched. " +
			"Conflicts are left as markers in the files: edit them away, then continue; commit and continue refuse while markers are left. Hooks do not run.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["status","diff","log","show","blame","branches","check","fetch","stash_list","tags","add","commit","branch","switch","merge","cherry_pick","rebase","continue","abort","pull","push","stash","stash_apply","stash_pop","stash_drop","tag","reset"]},"remote":{"type":"string"},"mode":{"type":"string","enum":["soft","mixed","hard"]},"index":{"type":"integer"},
"paths":{"type":"array","items":{"type":"string"}},"path":{"type":"string"},"revision":{"type":"string"},"message":{"type":"string"},
"branch":{"type":"string"},"rebase":{"type":"boolean"},"name":{"type":"string"},"create":{"type":"boolean"},"staged":{"type":"boolean"},"limit":{"type":"integer"}},"required":["action"]}`),
		Run: runGit,
		Summary: func(args json.RawMessage) string {
			parts := []string{argField(args, "action")}
			for _, field := range []string{"revision", "branch", "name", "path", "message"} {
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
		Action, Path, Revision, Message, Branch, Name, Remote, Mode string
		Index                                                       int
		Paths                                                       []string
		Create, Staged                                              bool
		Limit                                                       int
		Rebase                                                      *bool
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
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
		return repo.Log(args.Limit, args.Path)
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
		return repo.Branch(firstNonEmpty(args.Name, args.Branch))
	case "switch":
		return repo.Switch(firstNonEmpty(args.Branch, args.Name), args.Create)
	case "merge":
		return repo.Merge(args.Revision)
	case "cherry_pick":
		return repo.CherryPick(args.Revision)
	case "rebase":
		return repo.Rebase(args.Revision)
	case "fetch":
		return repo.Fetch(args.Remote)
	case "pull":
		return repo.Pull(gitops.PullOptions{Remote: args.Remote, Branch: args.Branch, Rebase: args.Rebase})
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
		return repo.Tag(firstNonEmpty(args.Name, args.Branch), args.Revision, args.Message)
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
