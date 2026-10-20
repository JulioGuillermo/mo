package tui

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/render"
)

const (
	fileCopy      = "file:copy"
	fileCopyImage = "file:image"
	fileCopyPath  = "file:path"
	fileView      = "file:view"
	fileOpen      = "file:open"
)

// fileUnder is the file at x, y of the conversation: one drawn there, as an
// image or under its caption, or a path of a file that exists written there.
func (m model) fileUnder(x, y int) (string, bool) {
	row := y + m.conversationOffset()
	lines := m.conversationLines().window(row, row+1)
	if row < 0 || len(lines) == 0 {
		return "", false
	}
	line := ansi.Strip(lines[0])
	if path, ok := m.printer.fileWith(line); ok {
		return path, true
	}
	return m.printer.pathAt(line, x)
}

// fileOptions are what a file offers: copying it as a file manager does,
// its picture, its path, and opening it here or in the system's app.
func fileOptions(path string) []huh.Option[string] {
	if path == "" {
		return nil
	}
	options := []huh.Option[string]{huh.NewOption("copy the file, to paste it as a file manager does", fileCopy)}
	if render.IsImageFile(path) {
		options = append(options, huh.NewOption("copy the image, to paste it in a document or a chat", fileCopyImage))
	}
	return append(options,
		huh.NewOption("copy its path", fileCopyPath),
		huh.NewOption("open it in mo", fileView),
		huh.NewOption("open it with the system's app", fileOpen),
	)
}

// fileMenu is what a right click on a file offers.
func (m model) fileMenu(path string) (tea.Model, tea.Cmd) {
	return m.showChoices(filepath.Base(path), path, fileOptions(path), func(m model, choice string) (tea.Model, tea.Cmd) {
		_, next, cmd := m.fileChoice(path, choice)
		return next, cmd
	})
}

// fileChoice does a file's option, telling whether choice was one.
func (m model) fileChoice(path, choice string) (bool, tea.Model, tea.Cmd) {
	switch choice {
	case fileCopy:
		return true, m, copyFile(path)
	case fileCopyImage:
		return true, m, copyPicture(path)
	case fileCopyPath:
		updated, cmd := m.flashCopied(path, "copied its path")
		return true, updated, cmd
	case fileView:
		updated, cmd := m.viewArgument(path)
		return true, updated, cmd
	case fileOpen:
		return true, m, openOutside(path)
	}
	return false, m, nil
}

// fileCopied says in the status line what reached the clipboard, or why
// it did not.
func (m model) fileCopied(msg fileCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.emitError(fmt.Errorf("%s: %w", map[string]string{"file": "copy the file", "image": "copy the image", "open": "open it"}[msg.what], msg.err))
	}
	flash := m.flashNote(map[string]string{"file": "copied the file", "image": "copied the image"}[msg.what])
	return m, flash
}

// flashCopied copies text and says so in the status line.
func (m model) flashCopied(text, note string) (tea.Model, tea.Cmd) {
	flash := m.flashNote(note)
	return m, tea.Batch(tea.SetClipboard(text), copyToSystem(text), flash)
}
