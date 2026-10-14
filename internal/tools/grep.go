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
	grepMaxMatches   = 20000
	grepMaxFileBytes = 2 << 20
)

var grepSkippedDirs = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "__pycache__": true}

const grepMaxContext = 10

func grepTool() Tool {
	return Tool{
		Name: "grep",
		Description: "Search files for a Go regexp, skipping hidden folders, node_modules and vendor. glob: file names, e.g. *.go. " +
			"show: lines (under their file, default), files or count. context: lines around each.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},"ignore_case":{"type":"boolean"},
"show":{"type":"string","enum":["lines","files","count"]},"context":{"type":"integer"}},"required":["pattern"]}`),
		Run:     runGrep,
		View:    true,
		Summary: func(args json.RawMessage) string { return argField(args, "pattern") },
	}
}

type grepOptions struct {
	re      *regexp.Regexp
	output  string
	context int
}

func runGrep(ctx context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
		Show       string `json:"show"`
		Context    int    `json:"context"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	switch args.Show {
	case "":
		args.Show = "lines"
	case "lines", "files", "count":
	default:
		return "", fmt.Errorf("show %q: use lines, files or count", args.Show)
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
	options := grepOptions{re: re, output: args.Show, context: min(max(args.Context, 0), grepMaxContext)}

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
		matches += grepFile(path, displayPath(env.WorkDir, path), options, &out, grepMaxMatches-matches)
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

// grepFile writes what matches in one file as options say, up to budget
// matches, and returns how many it counted: a file counts once when only
// names are asked for. Large and binary files are skipped.
func grepFile(path, shownPath string, options grepOptions, out *strings.Builder, budget int) int {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > grepMaxFileBytes {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil || looksBinary(data) {
		return 0
	}
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), grepMaxFileBytes)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	var matched []int
	for i, line := range lines {
		if options.re.MatchString(line) {
			matched = append(matched, i)
		}
	}
	switch {
	case len(matched) == 0:
		return 0
	case options.output == "files":
		fmt.Fprintln(out, shownPath)
		return 1
	case options.output == "count":
		fmt.Fprintf(out, "%s: %d\n", shownPath, len(matched))
		return 1
	}
	matched = matched[:min(len(matched), budget)]
	fmt.Fprintln(out, shownPath)
	isMatch := map[int]bool{}
	for _, i := range matched {
		isMatch[i] = true
	}
	shownUpTo := -1
	for _, i := range matched {
		from, to := max(i-options.context, shownUpTo+1), min(i+options.context, len(lines)-1)
		if shownUpTo >= 0 && from > shownUpTo+1 {
			out.WriteString("--\n")
		}
		for j := from; j <= to; j++ {
			separator := "-"
			if isMatch[j] {
				separator = ":"
			}
			line := lines[j]
			if options.context == 0 {
				line = strings.TrimSpace(line)
			}
			fmt.Fprintf(out, "%d%s %s\n", j+1, separator, line)
		}
		shownUpTo = max(shownUpTo, to)
	}
	return len(matched)
}

func displayPath(workDir, path string) string {
	relative, err := filepath.Rel(workDir, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return path
	}
	return relative
}
