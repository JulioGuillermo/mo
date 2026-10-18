package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

const (
	browseHelp = "↑↓ choose · / filter · enter open · esc back"
	parentName = ".."
)

// viewMatches are the commands /view offers for some files, each ready to run.
func viewMatches(paths []string) []command {
	var commands []command
	for _, path := range paths[:min(len(paths), viewShown)] {
		commands = append(commands, command{name: "view " + path, description: "file"})
	}
	return commands
}

// viewTargets are the files an argument of /view names when it is a search:
// @ and what to look for, a pattern with * or ?, or a word that is no path,
// by name anywhere under the folder. It tells whether it was a search.
func (m model) viewTargets(argument string) ([]string, bool) {
	files := m.files.list(m.session.WorkDir)
	switch {
	case strings.HasPrefix(argument, "@"):
		return fuzzyFiles(files, argument[1:]), true
	case isGlob(argument):
		return globFiles(files, argument), true
	}
	return nil, false
}

// viewArgument is /view <argument>: a file opens, a folder is browsed, a
// search shows the one file it finds or a list of those it does.
func (m model) viewArgument(argument string) (tea.Model, tea.Cmd) {
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return m.browse(m.session.WorkDir)
	}
	if paths, search := m.viewTargets(argument); search {
		return m.viewFound(argument, paths)
	}
	full := argument
	if strings.HasPrefix(full, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			full = filepath.Join(home, full[2:])
		}
	}
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.session.WorkDir, full)
	}
	info, err := os.Stat(full)
	switch {
	case err == nil && info.IsDir():
		return m.browse(full)
	case err == nil:
		return m.viewPath(argument)
	case !strings.ContainsAny(argument, `/\`):
		// Not here, and no folder in it: a name to look for in the project.
		return m.viewFound(argument, fuzzyFiles(m.files.list(m.session.WorkDir), argument))
	}
	return m.viewPath(argument)
}

// viewPath shows one file: drawn here when it is a picture or a diagram, else
// in the viewer.
func (m model) viewPath(path string) (tea.Model, tea.Cmd) {
	if drawnInline(path) {
		return m, m.view(path)
	}
	return m.openFile(path)
}

// viewFound shows what a search found: nothing is said, one file opens, and
// several are listed to pick from.
func (m model) viewFound(search string, paths []string) (tea.Model, tea.Cmd) {
	switch len(paths) {
	case 0:
		return m.emit(m.printer.Note("no file matches " + search))
	case 1:
		return m.viewPath(paths[0])
	}
	return m.pickFile(fmt.Sprintf("%d files match %s", len(paths), search), paths)
}

// pickFile lists files to filter and pick one to view.
func (m model) pickFile(title string, paths []string) (tea.Model, tea.Cmd) {
	picked := paths[0]
	var options []huh.Option[string]
	for _, path := range paths {
		options = append(options, huh.NewOption(path, path))
	}
	list := huh.NewSelect[string]().Title("View · " + title).
		Description("in " + m.session.WorkDir).Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) { return m.viewPath(picked) }
	back := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: browseHelp})
}

// browse lists a folder: .. to go up, its folders, then its files, to filter
// and pick: a folder lists that, a file is viewed.
func (m model) browse(dir string) (tea.Model, tea.Cmd) {
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return m.emitError(err)
	}
	var folders, files []string
	for _, entry := range entries {
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			folders = append(folders, entry.Name())
		} else {
			files = append(files, entry.Name())
		}
	}
	less := func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }
	slices.SortFunc(folders, less)
	slices.SortFunc(files, less)

	picked := ""
	var options []huh.Option[string]
	if parent := filepath.Dir(dir); parent != dir {
		options = append(options, huh.NewOption("▸ "+parentName, parentName))
		picked = parentName
	}
	for _, name := range folders {
		options = append(options, huh.NewOption("▸ "+name+"/", name+"/"))
	}
	for _, name := range files {
		options = append(options, huh.NewOption("  "+name, name))
	}
	if len(options) == 0 {
		return m.emit(m.printer.Note(m.shortDir(dir) + " is empty"))
	}
	if picked == "" {
		picked = options[0].Value
	}
	list := huh.NewSelect[string]().Title("View · " + m.shortDir(dir)).
		Description(fmt.Sprintf("%s · %d folders · %d files", m.shortDir(dir), len(folders), len(files))).
		Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) { return m.browseChoice(dir, picked) }
	back := func(m model) (tea.Model, tea.Cmd) { return m, nil }
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: browseHelp})
}

// browseChoice is what picking an entry of a folder's list does: .. lists the
// folder above, a folder lists that, a file is viewed.
func (m model) browseChoice(dir, picked string) (tea.Model, tea.Cmd) {
	switch {
	case picked == parentName:
		return m.browse(filepath.Dir(dir))
	case strings.HasSuffix(picked, "/"):
		return m.browse(filepath.Join(dir, strings.TrimSuffix(picked, "/")))
	}
	return m.viewPath(filepath.Join(dir, picked))
}

// shortDir is a folder as it reads best: from the working directory when it is
// inside it, with ~ for the home folder otherwise.
func (m model) shortDir(dir string) string {
	if relative, err := filepath.Rel(m.session.WorkDir, dir); err == nil && !strings.HasPrefix(relative, "..") {
		if relative == "." {
			return "./"
		}
		return "./" + filepath.ToSlash(relative) + "/"
	}
	if home, err := os.UserHomeDir(); err == nil {
		if relative, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(relative, "..") {
			return "~/" + filepath.ToSlash(relative)
		}
	}
	return dir
}
