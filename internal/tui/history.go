package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
)

// historyLimit is how many prompts and commands the history keeps.
const historyLimit = 1000

// inputHistory is what was sent before, prompts and commands, to bring back
// with the arrows: up on an empty input goes back, down comes forward.
type inputHistory struct {
	// entries go from the oldest to the newest.
	entries []string
	// shown is the entry shown while going through them, counted from 1;
	// 0 when not going through them.
	shown int
}

func historyFile() string {
	return filepath.Join(config.DataHome(), "mo", "history")
}

// loadHistory reads the history kept between sessions: one JSON string per
// line, so a prompt of several lines stays whole.
func loadHistory() inputHistory {
	var history inputHistory
	file, err := os.Open(historyFile())
	if err != nil {
		return history
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var entry string
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry != "" {
			history.entries = append(history.entries, entry)
		}
	}
	if len(history.entries) > historyLimit {
		history.entries = history.entries[len(history.entries)-historyLimit:]
	}
	return history
}

// add keeps what was sent, unless it repeats the last one, and stops going
// through the history. The file gets it off the UI goroutine.
func (h *inputHistory) add(entry string) tea.Cmd {
	h.shown = 0
	if entry == "" || (len(h.entries) > 0 && h.entries[len(h.entries)-1] == entry) {
		return nil
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > historyLimit {
		h.entries = h.entries[len(h.entries)-historyLimit:]
	}
	entries := append([]string(nil), h.entries...)
	return func() tea.Msg {
		_ = saveHistory(entries)
		return nil
	}
}

func saveHistory(entries []string) error {
	if err := os.MkdirAll(filepath.Dir(historyFile()), 0o700); err != nil {
		return err
	}
	var data []byte
	for _, entry := range entries {
		line, _ := json.Marshal(entry)
		data = append(append(data, line...), '\n')
	}
	temporary := historyFile() + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, historyFile())
}

// browsing tells whether the input shows an entry of the history as it
// was, so the arrows keep going through it.
func (m model) browsing() bool {
	shown := m.history.shown
	return shown > 0 && shown <= len(m.history.entries) && m.input.Value() == m.history.entries[shown-1]
}

// historyKey goes back or forward through the history: up on an empty
// input or while going through it, down while going through it, past the
// newest to an empty input again.
func (m model) historyKey(key string) (bool, tea.Model, tea.Cmd) {
	// Up on an empty input brings back what waits for the model, all of it,
	// to edit and send again: it goes first, as what is in view is that.
	if key == "up" && m.input.Value() == "" && len(m.queued) > 0 && m.session.Agent != nil {
		if taken := m.session.Agent.TakeQueued(); len(taken) > 0 {
			m.input.SetValue(joinQueued(taken))
			m.input.CursorEnd()
			m.queued = nil
			m.history.shown = 0
			m.suggestion = 0
			return true, m, nil
		}
	}
	browsing := m.browsing()
	switch {
	case key == "up" && (m.input.Value() == "" || browsing) && len(m.history.entries) > 0:
		shown := len(m.history.entries)
		if browsing {
			shown = max(m.history.shown-1, 1)
		}
		m.history.shown = shown
		m.input.SetValue(m.history.entries[shown-1])
		m.suggestion = 0
		return true, m, nil
	case key == "down" && browsing:
		if m.history.shown >= len(m.history.entries) {
			m.history.shown = 0
			m.input.Reset()
			return true, m, nil
		}
		m.history.shown++
		m.input.SetValue(m.history.entries[m.history.shown-1])
		m.suggestion = 0
		return true, m, nil
	}
	return false, m, nil
}
