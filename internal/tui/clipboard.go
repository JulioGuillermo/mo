package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
)

const clipboardTimeout = 2 * time.Second

// imageTypes are the clipboard types of a picture, best first, with the
// extension it is saved under.
var imageTypes = []struct{ mime, ext string }{
	{"image/png", ".png"}, {"image/jpeg", ".jpg"}, {"image/webp", ".webp"}, {"image/gif", ".gif"},
}

// clipboardMsg is what ctrl+v found: a picture saved to a file, or files
// copied in a file manager.
type clipboardMsg struct {
	image string
	files []string
	err   error
}

// clipboard is the program that reads the clipboard here: it lists the
// types the clipboard holds and gives the content of one.
type clipboard struct {
	types func(ctx context.Context) ([]string, error)
	read  func(ctx context.Context, mime string) ([]byte, error)
}

func systemClipboard() (clipboard, bool) {
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
	lines := func(out []byte, err error) ([]string, error) {
		return strings.Fields(string(out)), err
	}
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && found("wl-paste"):
		return clipboard{
			types: func(ctx context.Context) ([]string, error) { return lines(run(ctx, "wl-paste", "--list-types")) },
			read: func(ctx context.Context, mime string) ([]byte, error) {
				return run(ctx, "wl-paste", "--no-newline", "--type", mime)
			},
		}, true
	case os.Getenv("DISPLAY") != "" && found("xclip"):
		return clipboard{
			types: func(ctx context.Context) ([]string, error) {
				return lines(run(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"))
			},
			read: func(ctx context.Context, mime string) ([]byte, error) {
				return run(ctx, "xclip", "-selection", "clipboard", "-t", mime, "-o")
			},
		}, true
	case runtime.GOOS == "darwin":
		return macClipboard(run), true
	}
	return clipboard{}, false
}

// macClipboard reads pictures and copied files with AppleScript.
func macClipboard(run func(ctx context.Context, name string, args ...string) ([]byte, error)) clipboard {
	return clipboard{
		types: func(ctx context.Context) ([]string, error) {
			info, err := run(ctx, "osascript", "-e", "clipboard info")
			var types []string
			if strings.Contains(string(info), "PNGf") {
				types = append(types, "image/png")
			}
			if strings.Contains(string(info), "furl") {
				types = append(types, "text/uri-list")
			}
			return types, err
		},
		read: func(ctx context.Context, mime string) ([]byte, error) {
			if mime == "text/uri-list" {
				path, err := run(ctx, "osascript", "-e", "POSIX path of (the clipboard as «class furl»)")
				return []byte("file://" + strings.TrimSpace(string(path))), err
			}
			return run(ctx, "osascript", "-e", "set png to (the clipboard as «class PNGf»)",
				"-e", "set out to (do shell script \"mktemp\")",
				"-e", "set f to open for access POSIX file out with write permission",
				"-e", "write png to f", "-e", "close access f", "-e", "return out")
		},
	}
}

func found(program string) bool {
	_, err := exec.LookPath(program)
	return err == nil
}

// pasteClipboard reads what ctrl+v pastes: a picture or copied files become
// attachments, anything else is pasted as text the usual way.
func pasteClipboard() tea.Cmd {
	return func() tea.Msg {
		board, ok := systemClipboard()
		if !ok {
			return textarea.Paste()
		}
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		msg, attached := readAttachment(ctx, board)
		if !attached {
			return textarea.Paste()
		}
		return msg
	}
}

func readAttachment(ctx context.Context, board clipboard) (clipboardMsg, bool) {
	types, err := board.types(ctx)
	if err != nil {
		return clipboardMsg{}, false
	}
	if slices.Contains(types, "text/uri-list") || slices.Contains(types, "x-special/gnome-copied-files") {
		mime := "text/uri-list"
		if !slices.Contains(types, mime) {
			mime = "x-special/gnome-copied-files"
		}
		if list, err := board.read(ctx, mime); err == nil {
			if files := fileURIs(string(list)); len(files) > 0 {
				return clipboardMsg{files: files}, true
			}
		}
	}
	for _, image := range imageTypes {
		if !slices.Contains(types, image.mime) {
			continue
		}
		data, err := board.read(ctx, image.mime)
		if err != nil {
			return clipboardMsg{err: fmt.Errorf("reading the pasted image: %w", err)}, true
		}
		if runtime.GOOS == "darwin" {
			written := strings.TrimSpace(string(data))
			data, err = os.ReadFile(written)
			_ = os.Remove(written)
			if err != nil {
				return clipboardMsg{err: fmt.Errorf("reading the pasted image: %w", err)}, true
			}
		}
		path, err := savePaste(data, image.ext)
		return clipboardMsg{image: path, err: err}, true
	}
	return clipboardMsg{}, false
}

// fileURIs reads the local files of a uri-list, one per line.
func fileURIs(list string) []string {
	var files []string
	for line := range strings.Lines(list) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "file://") {
			continue
		}
		if parsed, err := url.Parse(line); err == nil {
			if info, err := os.Stat(parsed.Path); err == nil && info.Mode().IsRegular() {
				files = append(files, parsed.Path)
			}
		}
	}
	return files
}

// savePaste keeps a pasted picture in mo's cache, where the agent reads it.
func savePaste(data []byte, ext string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("the clipboard image is empty")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	dir := filepath.Join(cache, "mo", "pastes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "paste-"+time.Now().Format("20060102-150405.000")+ext)
	return path, os.WriteFile(path, data, 0o600)
}

// attach puts a short label for each file in the input; the prompt sent
// carries the path in its place.
func (m *model) attach(paths []string, image bool) {
	if m.attachments == nil {
		m.attachments = map[string]string{}
	}
	for _, path := range paths {
		m.input.InsertString(m.attachmentLabel(path, image) + " ")
	}
}

func (m *model) attachmentLabel(path string, pasted bool) string {
	kind := "file"
	if imageExtensions[strings.ToLower(filepath.Ext(path))] {
		kind = "image"
	}
	name := filepath.Base(path)
	if pasted {
		m.pastedImages++
		name = fmt.Sprintf("#%d", m.pastedImages)
	}
	label := fmt.Sprintf("[%s %s]", kind, name)
	for n := 2; m.attachments[label] != "" && m.attachments[label] != path; n++ {
		label = fmt.Sprintf("[%s %s %d]", kind, name, n)
	}
	m.attachments[label] = path
	return label
}

var imageExtensions = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// withAttachments swaps the labels in a prompt for the paths they stand for.
func (m *model) withAttachments(prompt string) string {
	var notes []string
	for label, path := range m.attachments {
		if note, ok := m.commentNotes[label]; ok && strings.Contains(prompt, label) {
			notes = append(notes, note)
			delete(m.fileComments, m.commentSources[label])
		}
		if strings.ContainsAny(path, " '\"\\") {
			path = `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
		}
		prompt = strings.ReplaceAll(prompt, label, path)
	}
	m.attachments, m.commentNotes, m.commentSources = nil, nil, nil
	if len(notes) > 0 {
		sort.Strings(notes)
		prompt += "\n\n" + strings.Join(notes, "\n\n")
	}
	return prompt
}

// pastedPaths turns pasted text that is only paths to files, as a dropped
// file is pasted, into attachments.
func (m *model) pastedPaths(content string) bool {
	files := agent.PastedFiles(content, m.session.WorkDir)
	if len(files) == 0 {
		return false
	}
	m.attach(files, false)
	return true
}

// eraseAttachment takes a whole attachment label out of the input when
// backspace is pressed right after it or delete right before it, and drops
// the file it stood for, instead of breaking the label one character at a
// time. It tells whether it did.
func (m *model) eraseAttachment(key string) bool {
	if len(m.attachments) == 0 {
		return false
	}
	rows := strings.Split(m.input.Value(), "\n")
	row, column := m.input.Line(), m.input.Column()
	if row < 0 || row >= len(rows) {
		return false
	}
	line := []rune(rows[row])
	column = min(column, len(line))
	before, after := string(line[:column]), string(line[column:])
	for label := range m.attachments {
		var start, end int
		switch {
		case key == "backspace" && strings.HasSuffix(before, label+" "):
			start, end = column-len([]rune(label))-1, column
		case key == "backspace" && strings.HasSuffix(before, label):
			start, end = column-len([]rune(label)), column
		case key == "delete" && strings.HasPrefix(after, label):
			start, end = column, column+len([]rune(label))
			if end < len(line) && line[end] == ' ' {
				end++
			}
		default:
			continue
		}
		rows[row] = string(line[:start]) + string(line[end:])
		m.input.SetValue(strings.Join(rows, "\n"))
		for range len(rows) - 1 - row {
			m.input.CursorUp()
		}
		m.input.SetCursorColumn(start)
		if !strings.Contains(m.input.Value(), label) {
			delete(m.attachments, label)
		}
		return true
	}
	return false
}

// attachmentBar shows the files the input holds as chips over it; a click
// on one takes it out of the input and drops it.
func (m model) attachmentBar() (string, []region) {
	value := m.input.Value()
	var labels []string
	for label := range m.attachments {
		if strings.Contains(value, label) {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		return "", nil
	}
	slices.Sort(labels)
	colors := dialogPalette(m.printer == nil || m.printer.isDark())
	chip := lipgloss.NewStyle().Background(colors.button).Foreground(colors.text)
	cross := chip.Foreground(lipgloss.Color("#f7768e")).Bold(true)
	columns := []string{dimStyle.Render("📎") + " "}
	var shownChips []string
	for _, label := range labels {
		shown := chip.Render(" "+strings.Trim(label, "[]")+" ") + cross.Render("× ")
		shownChips = append(shownChips, shown)
		if thumbnail := m.thumbnails[m.attachments[label]]; len(thumbnail) > 0 {
			shown = strings.Join(thumbnail, "\n") + "\n" + shown
		}
		columns = append(columns, shown, " ")
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, columns...)
	chipsRow := lipgloss.Height(bar) - 1
	var regions []region
	x := 2
	for i, label := range labels {
		chosen := label
		width := ansi.StringWidth(shownChips[i])
		regions = append(regions, region{y: chipsRow, x0: x, x1: x + width, do: func(m model) (tea.Model, tea.Cmd) {
			m.dropAttachment(chosen)
			return m, nil
		}})
		x += lipgloss.Width(columns[2*i+1]) + 1
	}
	return bar, regions
}

// dropAttachment takes every copy of a label out of the input, with the
// space after it, and forgets the file.
func (m *model) dropAttachment(label string) {
	value := strings.ReplaceAll(m.input.Value(), label+" ", "")
	m.input.SetValue(strings.ReplaceAll(value, label, ""))
	delete(m.attachments, label)
}

// readClipboardText reads the text on the clipboard, for vim's p.
func readClipboardText(ctx context.Context) (string, error) {
	var name string
	var args []string
	switch {
	case runtime.GOOS == "darwin":
		name = "pbpaste"
	case os.Getenv("WAYLAND_DISPLAY") != "" && found("wl-paste"):
		name, args = "wl-paste", []string{"--no-newline", "--type", "text/plain"}
	case os.Getenv("DISPLAY") != "" && found("xclip"):
		name, args = "xclip", []string{"-selection", "clipboard", "-o"}
	default:
		return "", fmt.Errorf("no clipboard program")
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
