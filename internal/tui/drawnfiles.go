package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// drawnFile is a file the conversation drew, with the lines it shows, as
// plain text, to find it by the line clicked.
type drawnFile struct {
	path  string
	shown []string
}

// keepFile keeps the lines a file was drawn as, its uploads left out.
func (p *printer) keepFile(path, drawn string) {
	var shown []string
	for _, line := range strings.Split(kittyUploads.ReplaceAllString(drawn, ""), "\n") {
		if plain := strings.TrimRight(ansi.Strip(line), " "); strings.TrimSpace(plain) != "" {
			shown = append(shown, plain)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files = append(p.files, drawnFile{path: path, shown: shown})
}

// fileWith is the file drawn as line, the latest when several were.
func (p *printer) fileWith(line string) (string, bool) {
	line = strings.TrimRight(line, " ")
	if strings.TrimSpace(line) == "" {
		return "", false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.files) - 1; i >= 0; i-- {
		if slices.Contains(p.files[i].shown, line) {
			return p.files[i].path, true
		}
	}
	return "", false
}

// absolute is a path as the conversation names it, from the working
// directory or the home folder.
func (p *printer) absolute(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(path) {
		return filepath.Join(p.workDir, path)
	}
	return path
}

// pathAt is the path of a file that exists in the word at column x of a
// line: a word as the shell would split it, without the quotes, brackets
// and punctuation around it, file:// and :line:column taken away.
func (p *printer) pathAt(line string, x int) (string, bool) {
	word := wordAt(line, x)
	word = strings.Trim(word, "`'\"()[]{}<>,;")
	word = strings.TrimSuffix(word, ".")
	word = strings.TrimPrefix(word, "file://")
	if word == "" {
		return "", false
	}
	for _, candidate := range []string{word, withoutPosition(word)} {
		full := p.absolute(candidate)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full, true
		}
	}
	return "", false
}

// withoutPosition takes :line or :line:column off a path, as compilers and
// grep name a place in a file.
func withoutPosition(path string) string {
	for range 2 {
		at := strings.LastIndexByte(path, ':')
		if at < 0 || strings.Trim(path[at+1:], "0123456789") != "" || at == len(path)-1 {
			break
		}
		path = path[:at]
	}
	return path
}

// wordAt is the word, between spaces, that column x of line falls in.
func wordAt(line string, x int) string {
	runes := []rune(line)
	column, at := 0, -1
	for i, r := range runes {
		width := max(ansi.StringWidth(string(r)), 1)
		if x >= column && x < column+width {
			at = i
			break
		}
		column += width
	}
	if at < 0 || runes[at] == ' ' {
		return ""
	}
	start, end := at, at
	for start > 0 && runes[start-1] != ' ' {
		start--
	}
	for end < len(runes)-1 && runes[end+1] != ' ' {
		end++
	}
	return string(runes[start : end+1])
}
