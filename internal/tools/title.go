package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

const titleToolName = "title"

// Titles carries the title the model gives the conversation to whoever
// keeps it, the interactive UI. Without one there is no title tool.
type Titles struct {
	mu    sync.Mutex
	onSet func(title string)
}

func NewTitles() *Titles {
	return &Titles{}
}

// OnSet says what to do with a title the model gives.
func (t *Titles) OnSet(onSet func(title string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onSet = onSet
}

func (t *Titles) set(title string) bool {
	t.mu.Lock()
	onSet := t.onSet
	t.mu.Unlock()
	if onSet == nil {
		return false
	}
	onSet(title)
	return true
}

func titleTool() Tool {
	return Tool{
		Name:        titleToolName,
		Description: "Rename this conversation, when the user asks you to. title: a few words saying what it is about, in the user's language.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
		Run: func(_ context.Context, env Env, raw json.RawMessage) (string, error) {
			title := strings.Join(strings.Fields(argField(raw, "title")), " ")
			switch {
			case title == "":
				return "", errors.New("title is empty")
			case env.Titles == nil || !env.Titles.set(title):
				return "", errors.New("this conversation cannot be renamed here")
			}
			return "the conversation is now titled: " + title, nil
		},
		Summary: func(args json.RawMessage) string { return argField(args, "title") },
	}
}
