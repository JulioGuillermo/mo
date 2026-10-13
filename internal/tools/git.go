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
var gitChanges = []string{"add", "commit", "branch", "switch", "merge", "cherry_pick", "continue", "abort"}

func gitTool() Tool {
	return Tool{
		Name: "git",
		Description: "Git in the working directory, without the git command. Read: status, diff (staged: the index), log (path, limit), show (revision), blame (path), branches, " +
			"check (conflicts left). Change: add (paths; none: all), commit (message), branch (name), switch (branch, create), merge (revision), cherry_pick (revision), continue, abort. " +
			"Conflicts are left as markers in the files: edit them away, then continue; commit and continue refuse while markers are left. Hooks do not run.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["status","diff","log","show","blame","branches","check","add","commit","branch","switch","merge","cherry_pick","continue","abort"]},
"paths":{"type":"array","items":{"type":"string"}},"path":{"type":"string"},"revision":{"type":"string"},"message":{"type":"string"},
"branch":{"type":"string"},"name":{"type":"string"},"create":{"type":"boolean"},"staged":{"type":"boolean"},"limit":{"type":"integer"}},"required":["action"]}`),
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
	}
}

func runGit(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Action, Path, Revision, Message, Branch, Name string
		Paths                                         []string
		Create, Staged                                bool
		Limit                                         int
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
