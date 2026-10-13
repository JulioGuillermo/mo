package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jg/mo/internal/llm"
)

const (
	warningOutsideWorkDir = "outside the working directory"
	warningProtectedPath  = "protected path"
	warningGlobalMemory   = "memory shared by every project"
)

// Warning names the risk of a call that changes a path outside the working
// directory or a protected one (mo's config, a .git directory, a hidden entry
// of the home directory), or that changes a global memory. Such calls are
// confirmed every time.
func (r *Registry) Warning(call llm.ToolCall) string {
	tool, ok := r.find(call.Name)
	if !ok || checkArgNames(tool, call.Args) != nil {
		return ""
	}
	if tool.Name == "memory" && changesGlobalMemory(call.Args) {
		return warningGlobalMemory
	}
	if !tool.ChangesPathArg {
		return ""
	}
	path := changedPath(call.Name, r.env.Resolve(argField(call.Args, "path")))
	if !isInside(path, physicalPath(r.env.WorkDir)) {
		return warningOutsideWorkDir
	}
	if r.isProtected(path) {
		return warningProtectedPath
	}
	return ""
}

func changesGlobalMemory(args json.RawMessage) bool {
	action := argField(args, "action")
	return argField(args, "scope") == memoryScopeGlobal && (action == "write" || action == "delete")
}

func (r *Registry) isProtected(path string) bool {
	for _, protected := range r.env.ProtectedPaths {
		if isInside(path, physicalPath(protected)) {
			return true
		}
	}
	if slices.Contains(strings.Split(path, string(filepath.Separator)), ".git") {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(physicalPath(home), path)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return false
	}
	return strings.HasPrefix(relative, ".")
}

// changedPath is where a call really lands once symlinks are followed. rm
// removes a link itself, so only the directories above it are resolved.
func changedPath(tool, path string) string {
	if tool == "rm" {
		return filepath.Join(physicalPath(filepath.Dir(path)), filepath.Base(path))
	}
	return physicalPath(path)
}

// physicalPath resolves the symlinks of the longest part of path that exists
// and appends the rest unchanged.
func physicalPath(path string) string {
	existing := path
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return path
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
}

func isInside(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, "../")
}

// checkArgNames rejects any argument whose name is not exactly one of the
// schema's properties, when the schema lists them, unless the tool takes
// loose arguments, as MCP tools do. JSON decoding into a
// struct matches names without case, so "Path" would otherwise reach the
// tool unchecked.
func checkArgNames(tool Tool, raw json.RawMessage) error {
	if tool.LooseArgs {
		return nil
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema, &schema); err != nil {
		return err
	}
	if schema.Properties == nil {
		return nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	for name := range args {
		if _, known := schema.Properties[name]; !known {
			return fmt.Errorf("unknown argument %q", name)
		}
	}
	return nil
}
