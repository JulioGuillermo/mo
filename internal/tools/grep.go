package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const grepMaxMatches = 20000

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
		found, err := grepFile(ctx, path, displayPath(env.WorkDir, path), options, &out, grepMaxMatches-matches)
		if err != nil {
			return err
		}
		matches += found
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
// names are asked for. It reads the file a line at a time, so a file of
// any size is searched without being held, and stops as soon as it has
// what was asked; binary files, told by their start, are skipped.
func grepFile(ctx context.Context, path, shownPath string, options grepOptions, out *strings.Builder, budget int) (int, error) {
	// Stat first: opening a FIFO would wait for a writer.
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return 0, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, nil
	}
	defer file.Close()
	head := make([]byte, readSniff)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	if _, binary := sniffBinary(head); binary {
		return 0, nil
	}
	search := &grepSearch{options: options, out: out, path: shownPath, budget: budget, shownUpTo: -1}
	err = eachLine(ctx, io.MultiReader(bytes.NewReader(head), file), 0, search.line)
	if search.matches > 0 && options.output == "count" {
		fmt.Fprintf(out, "%s: %d\n", shownPath, search.matches)
		return 1, err
	}
	if search.matches > 0 && options.output == "files" {
		return 1, err
	}
	return search.matches, err
}

// grepSearch is the search of one file as its lines go by: the lines
// before the next match kept for its context, and the lines after the
// last one still to show.
type grepSearch struct {
	options   grepOptions
	out       *strings.Builder
	path      string
	budget    int
	matches   int
	before    []grepLine
	after     int
	shownUpTo int
}

type grepLine struct {
	number int
	text   string
}

func (s *grepSearch) line(number int, text string, _ int) bool {
	options := s.options
	// Only the lines shown are limited; a count goes to the end.
	matched := (options.output != "lines" || s.matches < s.budget) && options.re.MatchString(text)
	switch {
	case matched && options.output == "files":
		s.matches = 1
		fmt.Fprintln(s.out, s.path)
		return false
	case matched && options.output == "count":
		s.matches++
		return true
	case matched:
		if s.matches == 0 {
			fmt.Fprintln(s.out, s.path)
		}
		from := number
		for _, kept := range s.before {
			if kept.number > s.shownUpTo {
				from = kept.number
				break
			}
		}
		if s.shownUpTo >= 0 && from > s.shownUpTo+1 {
			s.out.WriteString("--\n")
		}
		for _, kept := range s.before {
			if kept.number > s.shownUpTo {
				s.write(kept.number, "-", kept.text)
			}
		}
		s.write(number, ":", text)
		s.matches++
		s.after = options.context
	case s.after > 0:
		s.write(number, "-", text)
		s.after--
	case s.matches >= s.budget && options.output == "lines":
		return false
	}
	if options.context > 0 {
		if len(s.before) == options.context {
			s.before = s.before[1:]
		}
		s.before = append(s.before, grepLine{number, text})
	}
	return true
}

func (s *grepSearch) write(number int, separator, text string) {
	if s.options.context == 0 {
		text = strings.TrimSpace(text)
	}
	fmt.Fprintf(s.out, "%d%s %s\n", number+1, separator, text)
	s.shownUpTo = number
}

func displayPath(workDir, path string) string {
	relative, err := filepath.Rel(workDir, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return path
	}
	return relative
}
