// Package sessions saves conversations to resume them later, each in its own
// file, and lists those of a working directory.
package sessions

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jg/mo/internal/llm"
)

const titleLength = 80

// Record is a saved conversation. Parent is the conversation it was forked
// from, and Forked the prompt it was forked before.
type Record struct {
	Info
	History []llm.Message `json:"history"`
	// Agents are the subagents of the conversation, with all they did.
	Agents []AgentRecord `json:"agents,omitempty"`
}

// AgentRecord is a subagent as it was saved: who started it, its task,
// how it ended and its whole conversation.
type AgentRecord struct {
	ID      int           `json:"id"`
	Parent  int           `json:"parent"`
	Depth   int           `json:"depth"`
	Name    string        `json:"name"`
	Task    string        `json:"task"`
	State   string        `json:"state"`
	Error   string        `json:"error,omitempty"`
	Result  string        `json:"result,omitempty"`
	History []llm.Message `json:"history"`
	Usage   llm.Usage     `json:"usage"`
}

// Info is what a list shows of a conversation, without its messages.
type Info struct {
	ID      string    `json:"id"`
	WorkDir string    `json:"work_dir"`
	Title   string    `json:"title"`
	Parent  string    `json:"parent,omitempty"`
	Forked  string    `json:"forked,omitempty"`
	Prompts int       `json:"prompts"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Usage   llm.Usage `json:"usage"`
}

// NewID names a new conversation: when it started, and a random part so
// two started in the same second stay apart.
func NewID() string {
	random := make([]byte, 3)
	_, _ = rand.Read(random)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(random)
}

// Title is the first line of the first prompt, cut short.
func Title(prompt string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
	if runes := []rune(line); len(runes) > titleLength {
		return string(runes[:titleLength-1]) + "…"
	}
	return line
}

// Save writes a conversation, replacing its file whole. The folder and file
// are readable by the user only: they hold everything said and read.
func Save(dir string, record Record) error {
	if record.ID == "" || strings.ContainsAny(record.ID, `/\.`) {
		return fmt.Errorf("bad conversation id %q", record.ID)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, record.ID+".json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// Load reads a saved conversation.
func Load(dir, id string) (Record, error) {
	var record Record
	if strings.ContainsAny(id, `/\`) {
		return record, fmt.Errorf("bad conversation id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("conversation %s: %w", id, err)
	}
	return record, nil
}

// List returns the conversations saved for workDir, the latest first. A file
// that cannot be read is skipped.
func List(dir, workDir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var infos []Info
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var info Info
		if json.Unmarshal(data, &info) != nil || info.WorkDir != workDir {
			continue
		}
		infos = append(infos, info)
	}
	slices.SortFunc(infos, func(a, b Info) int { return b.Updated.Compare(a.Updated) })
	return infos, nil
}

// JobsDir is where the commands run in a conversation keep their output,
// beside its file, so the output lasts as long as the conversation.
func JobsDir(dir, id string) string {
	return filepath.Join(dir, id, "jobs")
}

// Delete removes a conversation and the output of its commands.
func Delete(dir, id string) error {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return fmt.Errorf("bad conversation id %q", id)
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.RemoveAll(filepath.Join(dir, id))
}

// Older returns the conversations of workDir last updated before when.
func Older(dir, workDir string, when time.Time) ([]Info, error) {
	infos, err := List(dir, workDir)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(infos, func(info Info) bool { return !info.Updated.Before(when) }), nil
}

// ShareJobs gives a fork the output of the commands run before it, as hard
// links, or copies where links cannot be made: each conversation keeps its
// own, and deleting one leaves the other whole.
func ShareJobs(dir, from, to string) error {
	if from == "" || to == "" {
		return nil
	}
	source, target := JobsDir(dir, from), JobsDir(dir, to)
	entries, err := os.ReadDir(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tail") {
			continue
		}
		from, to := filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())
		if os.Link(from, to) == nil {
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// UIFile is where the interfaces a conversation's model drew are kept,
// with what the user and their scripts did in them.
func UIFile(dir, id string) string {
	return filepath.Join(dir, id, "uis.json")
}
