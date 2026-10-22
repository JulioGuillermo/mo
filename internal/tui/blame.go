package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/gitops"
)

const blameGap = 3

// blameReadMsg brings who last changed each line of a file in the viewer.
type blameReadMsg struct {
	file  string
	blame []gitops.LineBlame
	err   error
}

// repoFile is the repository a file is in and its path there, as git
// names it.
func repoFile(full string) (*gitops.Repo, string, error) {
	repo, err := gitops.Open(filepath.Dir(full))
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(repo.Root(), full)
	if err != nil {
		return nil, "", err
	}
	return repo, filepath.ToSlash(relative), nil
}

// viewedPath is the file the viewer shows, from the root of the disk.
func (m model) viewedPath() string {
	v := m.viewer
	full := v.file
	if v.changes != nil {
		full = filepath.Join(v.changes.root, v.diffFile)
	}
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.session.WorkDir, full)
	}
	return full
}

// readBlame reads in the background who last changed each line of the
// file the viewer shows.
func (m model) readBlame() tea.Cmd {
	v := m.viewer
	if v.blame != nil || v.blameNote != "" {
		return nil
	}
	v.blameNote = "reading git blame…"
	file, full := v.file, m.viewedPath()
	return func() tea.Msg {
		repo, path, err := repoFile(full)
		if err != nil {
			return blameReadMsg{file: file, err: err}
		}
		blame, err := repo.BlameFile(path)
		return blameReadMsg{file: file, blame: blame, err: err}
	}
}

func (m model) blameRead(msg blameReadMsg) (tea.Model, tea.Cmd) {
	v := m.viewer
	if v == nil || v.file != msg.file {
		return m, nil
	}
	switch {
	case msg.err != nil:
		v.blameNote = "no git blame: " + msg.err.Error()
	default:
		v.blame, v.blameNote = msg.blame, ""
	}
	return m, nil
}

// toggleBlame shows who last changed the cursor's line, or stops.
func (m model) toggleBlame() (tea.Model, tea.Cmd) {
	v := m.viewer
	v.blameOn = !v.blameOn
	if !v.blameOn {
		return m, nil
	}
	if strings.HasPrefix(v.blameNote, "no git blame") {
		v.blameNote = ""
	}
	return m, m.readBlame()
}

// blameOf is what the viewer says of who last changed a line.
func (v *outputViewer) blameOf(line int) string {
	switch {
	case !v.blameOn:
		return ""
	case v.blame == nil:
		return v.blameNote
	case line >= len(v.blame):
		return ""
	}
	blame := v.blame[line]
	if !blame.Committed() {
		return "not committed yet"
	}
	return blame.Author + " · " + ago(blame.When) + " · " + blame.Short() + " " + blame.Subject
}

// withBlame puts what is known of who changed a line after it, in grey,
// when there is room for some of it.
func withBlame(line, blame string, width int) string {
	room := width - ansi.StringWidth(line) - blameGap
	if blame == "" || room < 8 {
		return line
	}
	return line + strings.Repeat(" ", blameGap) + dimStyle.Render(ansi.Truncate(blame, room, "…"))
}
