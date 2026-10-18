package tui

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// viewShown is how many files a search offers after /view.
const viewShown = 50

// fuzzyFiles are the files whose path holds what was typed, best first:
// those whose name starts with it, then those whose name holds it, then those
// whose path does, and last those holding its letters in order; the shorter
// path first among equals. Nothing typed offers them all.
func fuzzyFiles(files []string, typed string) []string {
	query := strings.ToLower(typed)
	type found struct {
		path  string
		score int
	}
	var matches []found
	for _, path := range files {
		lower := strings.ToLower(path)
		base := strings.ToLower(filepath.Base(path))
		switch {
		case query == "":
			matches = append(matches, found{path, 3})
		case strings.HasPrefix(base, query):
			matches = append(matches, found{path, 0})
		case strings.Contains(base, query):
			matches = append(matches, found{path, 1})
		case strings.Contains(lower, query):
			matches = append(matches, found{path, 2})
		case inOrder(lower, query):
			matches = append(matches, found{path, 3})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return len(matches[i].path) < len(matches[j].path)
	})
	paths := make([]string, len(matches))
	for i, match := range matches {
		paths[i] = match.path
	}
	return paths
}

// isGlob tells whether a word is a pattern for file names: it has * or ?.
func isGlob(word string) bool {
	return strings.ContainsAny(word, "*?")
}

// globRegexp is a pattern for file names as a regular expression: * is any
// characters of a name, ** any, folders too, and ? one character of a name.
// Capitals do not matter.
func globRegexp(pattern string) (*regexp.Regexp, error) {
	var expression strings.Builder
	expression.WriteString("(?i)^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					// **/ is any folders, or none.
					i++
					expression.WriteString(`(?:.*/)?`)
					continue
				}
				expression.WriteString(`.*`)
				continue
			}
			expression.WriteString(`[^/]*`)
		case '?':
			expression.WriteString(`[^/]`)
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	expression.WriteString("$")
	return regexp.Compile(expression.String())
}

// globFiles are the files a pattern names, by their paths from where they
// are listed. A pattern with no / is matched against the name of a file at
// any depth, as *.go is; one with a / against the whole path.
func globFiles(files []string, pattern string) []string {
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "./")
	re, err := globRegexp(pattern)
	if err != nil {
		return nil
	}
	byName := !strings.Contains(pattern, "/")
	var matches []string
	for _, path := range files {
		subject := path
		if byName {
			subject = filepath.Base(path)
		}
		if re.MatchString(filepath.ToSlash(subject)) {
			matches = append(matches, path)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if depth := strings.Count(matches[i], "/") - strings.Count(matches[j], "/"); depth != 0 {
			return depth < 0
		}
		return matches[i] < matches[j]
	})
	return matches
}
