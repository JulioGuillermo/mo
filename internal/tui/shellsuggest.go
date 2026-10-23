package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

const shellShown = 50

// shellWord is the word being typed of a command after !, and whether it
// is the program, the first word of the command or after | ; &.
func shellWord(command string) (word string, program bool) {
	start := strings.LastIndexAny(command, " \t") + 1
	word = command[start:]
	before := strings.TrimRight(command[:start], " \t")
	program = before == "" || strings.HasSuffix(before, "|") || strings.HasSuffix(before, ";") || strings.HasSuffix(before, "&")
	return word, program
}

// shellSuggestions are what the word being typed after ! may become: the
// programs on the PATH for the first word, files and folders after it, or
// for one that is a path.
func (m model) shellSuggestions(command string) []command {
	if strings.Contains(command, "\n") {
		return nil
	}
	word, program := shellWord(command)
	if word == "" {
		return nil
	}
	if program && !strings.ContainsAny(word, `/\~`) && !strings.HasPrefix(word, ".") {
		return programSuggestions(word)
	}
	return m.shellPathSuggestions(word)
}

// programSuggestions are the programs whose names start with what was
// typed, the shortest first, as the one meant is often the plainest.
func programSuggestions(typed string) []command {
	var names []string
	for _, name := range programsOnPath() {
		if hasPrefixFold(name, typed) && name != typed {
			names = append(names, name)
		}
	}
	slices.SortStableFunc(names, func(a, b string) int { return len(a) - len(b) })
	var matches []command
	for _, name := range names[:min(len(names), shellShown)] {
		matches = append(matches, command{name: name, description: "program", shell: true})
	}
	return matches
}

// hasPrefixFold is strings.HasPrefix, capitals not mattering on Windows,
// whose names do not tell them apart.
func hasPrefixFold(name, prefix string) bool {
	if runtime.GOOS == "windows" {
		return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
	}
	return strings.HasPrefix(name, prefix)
}

// shellPathSuggestions are the files and folders a path being typed may
// become, folders first; hidden ones only when a dot is typed.
func (m model) shellPathSuggestions(word string) []command {
	dir, prefix := filepath.Split(word)
	entries, err := os.ReadDir(m.absolutePath(orDefault(dir, ".")))
	if err != nil {
		return nil
	}
	var folders, files []command
	for _, entry := range entries {
		name := entry.Name()
		if !hasPrefixFold(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(m.absolutePath(orDefault(dir, ".")), name)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			folders = append(folders, command{name: dir + name + string(filepath.Separator), description: "folder", shell: true, more: true})
			continue
		}
		files = append(files, command{name: dir + name, description: "file", shell: true})
	}
	found := append(folders, files...)
	return found[:min(len(found), shellShown)]
}

// completeShellWord puts the suggestion chosen in place of the word being
// typed: a folder to go on typing in, else with a space after it.
func (m *model) completeShellWord(chosen command) {
	value := m.input.Value()
	_, command, _ := typedShell(value)
	word, _ := shellWord(command)
	completed := value[:len(value)-len(word)] + shellQuoted(chosen.name)
	if !chosen.more {
		completed += " "
	}
	m.input.SetValue(completed)
	m.input.CursorEnd()
	m.suggestion = 0
}

// shellQuoted is a word as the shell takes it whole, quoted when it has
// spaces or quotes in it.
func shellQuoted(word string) string {
	if !strings.ContainsAny(word, " '\"$&|;()<>") {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

var pathPrograms = sync.OnceValue(findPrograms)

func programsOnPath() []string {
	return pathPrograms()
}

// findPrograms are the names of the programs in the folders of the PATH,
// sorted, each once; on Windows without the extension that makes them one.
func findPrograms() []string {
	extensions := strings.Split(strings.ToLower(os.Getenv("PATHEXT")), ";")
	seen := map[string]bool{}
	var names []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name, ok := pathProgram(dir, entry, extensions)
			if ok && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return names
}

func pathProgram(dir string, entry os.DirEntry, extensions []string) (string, bool) {
	name := entry.Name()
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(name))
		if extension == "" || !slices.Contains(extensions, extension) {
			return "", false
		}
		return strings.TrimSuffix(name, filepath.Ext(name)), true
	}
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", false
	}
	return name, true
}
