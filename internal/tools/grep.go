package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	grepMaxMatches   = 200
	grepMaxFileBytes = 2 << 20
	grepMaxLineBytes = 300
)

var grepSkippedDirs = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "__pycache__": true}

func grepTool() Tool {
	return Tool{
		Name:        "grep",
		Description: "Search file contents with a Go regexp. Skips hidden dirs, node_modules, vendor. glob filters file names, e.g. *.go.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},"ignore_case":{"type":"boolean"}},"required":["pattern"]}`),
		Run:     runGrep,
		Summary: func(args json.RawMessage) string { return argField(args, "pattern") },
	}
}

func runGrep(ctx context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	pattern := args.Pattern
	if args.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	if args.Path == "" {
		args.Path = "."
	}
	root := env.Resolve(args.Path)

	var out strings.Builder
	matches := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			if path != root && isSkippedDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if args.Glob != "" {
			if matched, _ := filepath.Match(args.Glob, entry.Name()); !matched {
				return nil
			}
		}
		matches += grepFile(path, displayPath(env.WorkDir, path), re, &out, grepMaxMatches-matches)
		if matches >= grepMaxMatches {
			fmt.Fprintf(&out, "[stopped at %d matches]\n", grepMaxMatches)
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return out.String(), walkErr
	}
	if matches == 0 {
		return "no matches", nil
	}
	return out.String(), nil
}

func isSkippedDir(name string) bool {
	return strings.HasPrefix(name, ".") || grepSkippedDirs[name]
}

// grepFile writes up to budget matching lines of one file and returns how
// many it wrote. Large and binary files are skipped.
func grepFile(path, shownPath string, re *regexp.Regexp, out *strings.Builder, budget int) int {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > grepMaxFileBytes {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil || looksBinary(data) {
		return 0
	}
	found := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), grepMaxFileBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		if !re.MatchString(line) {
			continue
		}
		if len(line) > grepMaxLineBytes {
			line = cutAtRune(line, grepMaxLineBytes) + "…"
		}
		fmt.Fprintf(out, "%s:%d: %s\n", shownPath, lineNumber, strings.TrimSpace(line))
		found++
		if found == budget {
			break
		}
	}
	return found
}

func displayPath(workDir, path string) string {
	relative, err := filepath.Rel(workDir, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return path
	}
	return relative
}
