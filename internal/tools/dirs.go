package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func lsTool() Tool {
	return Tool{
		Name:        "ls",
		Description: "List a directory; folders end in /.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Run:         runLs,
		View:        true,
		Summary:     func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runLs(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Path == "" {
		args.Path = "."
	}
	entries, err := os.ReadDir(env.Resolve(args.Path))
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "(empty directory)", nil
	}
	var out strings.Builder
	for _, entry := range entries {
		out.WriteString(entry.Name())
		if entry.IsDir() {
			out.WriteByte('/')
		}
		out.WriteByte('\n')
	}
	return out.String(), nil
}

func mkdirTool() Tool {
	return Tool{
		ChangesPathArg: true,
		Name:           "mkdir",
		Description:    "Create a folder.",
		Schema:         json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		Run:            runMkdir,
		Summary:        func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runMkdir(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if err := os.MkdirAll(env.Resolve(args.Path), 0o755); err != nil {
		return "", err
	}
	return "created", nil
}

func rmTool() Tool {
	return Tool{
		ChangesPathArg: true,
		Name:           "rm",
		Description:    "Delete a file, or a folder with recursive.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string"},"recursive":{"type":"boolean"}},"required":["path"]}`),
		Run:     runRm,
		Summary: func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runRm(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	path := env.Resolve(args.Path)
	if isProtectedPath(changedPath("rm", path), physicalPath(env.WorkDir)) {
		return "", fmt.Errorf("refusing to delete %s", path)
	}
	remove := os.Remove
	if args.Recursive {
		remove = os.RemoveAll
	}
	if _, err := os.Lstat(path); err != nil {
		return "", err
	}
	if err := remove(path); err != nil {
		return "", err
	}
	return "deleted", nil
}

// isProtectedPath refuses top-level directories such as /usr, and the home
// and working directories along with everything above them.
func isProtectedPath(path, workDir string) bool {
	if strings.Count(path, string(filepath.Separator)) <= 1 {
		return true
	}
	home, _ := os.UserHomeDir()
	for _, protected := range []string{physicalPath(home), workDir} {
		if protected != "" && isInside(protected, path) {
			return true
		}
	}
	return false
}
