package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/jg/mo/internal/sessions"
)

const (
	BtwLogName       = "btw_log"
	btwListAnswerCut = 160
	btwCallsOutput   = 2000
)

// Asides are the side questions of a conversation, asked with /btw, kept
// for the btw_log tool to read and the conversation to save.
type Asides struct {
	mu   sync.Mutex
	list []sessions.Aside
}

func NewAsides() *Asides {
	return &Asides{}
}

// Add keeps an aside, numbered after the last, and gives its number.
func (a *Asides) Add(aside sessions.Aside) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	aside.ID = len(a.list) + 1
	a.list = append(a.list, aside)
	return aside.ID
}

// All are the asides kept, the first asked first.
func (a *Asides) All() []sessions.Aside {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.list)
}

// Restore puts back the asides of a conversation resumed, none for a new one.
func (a *Asides) Restore(list []sessions.Aside) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.list = slices.Clone(list)
}

func (a *Asides) find(id int) (sessions.Aside, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	at := slices.IndexFunc(a.list, func(aside sessions.Aside) bool { return aside.ID == id })
	if at < 0 {
		return sessions.Aside{}, false
	}
	return a.list[at], true
}

func btwLogTool() Tool {
	return Tool{
		Name: BtwLogName,
		Description: "The side questions the user asked with /btw, and side tasks with /btw!, while the conversation went on; they are not in it. " +
			"list: each with its number, question, the start of its answer, and what a /btw! changed. read: one whole by its number; calls: true adds the tool calls it made and their results.",
		Schema: json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","read"]},"id":{"type":"integer"},"calls":{"type":"boolean"}},"required":["action"]}`),
		Run: func(_ context.Context, env Env, raw json.RawMessage) (string, error) {
			if env.Asides == nil {
				return "", errors.New("there are no side questions here")
			}
			var args struct {
				Action string `json:"action"`
				ID     int    `json:"id"`
				Calls  bool   `json:"calls"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", err
			}
			switch args.Action {
			case "list":
				return listAsides(env.Asides.All()), nil
			case "read":
				aside, ok := env.Asides.find(args.ID)
				if !ok {
					return "", fmt.Errorf("no side question %d: list them first", args.ID)
				}
				return readAside(aside, args.Calls), nil
			}
			return "", fmt.Errorf("action %q: use list or read", args.Action)
		},
		Summary: func(args json.RawMessage) string {
			if id := argField(args, "id"); id != "" {
				return argField(args, "action") + " " + id
			}
			return argField(args, "action")
		},
	}
}

func listAsides(asides []sessions.Aside) string {
	if len(asides) == 0 {
		return "no side questions yet"
	}
	var lines []string
	for _, aside := range asides {
		line := fmt.Sprintf("#%d %s «%s» → %s", aside.ID, asideCommand(aside), shortLine(aside.Question, btwListAnswerCut), asideOutcome(aside))
		if len(aside.Changes) > 0 {
			line += " · did: " + strings.Join(aside.Changes, "; ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func readAside(aside sessions.Aside, calls bool) string {
	text := fmt.Sprintf("#%d %s, asked %s\nQuestion: %s\n", aside.ID, asideCommand(aside), aside.At.Format("2006-01-02 15:04"), aside.Question)
	if calls {
		for _, turn := range turnsOf(aside.History, btwCallsOutput) {
			text += turn.text
		}
	} else if aside.Answer != "" {
		text += "Answer: " + aside.Answer + "\n"
	}
	if aside.Ending != "" {
		text += "Ended: " + aside.Ending + "\n"
	}
	if len(aside.Changes) > 0 {
		text += "Did:\n- " + strings.Join(aside.Changes, "\n- ") + "\n"
	}
	return text
}

// asideTurns are the side questions as turns of their own, for the
// conversations' search to find.
func asideTurns(asides []sessions.Aside) []recalledTurn {
	var turns []recalledTurn
	for _, aside := range asides {
		prompt := asideCommand(aside) + " " + aside.Question
		turns = append(turns, recalledTurn{prompt: prompt, text: "User: " + prompt + "\n" + readAside(aside, false)})
	}
	return turns
}

func asideCommand(aside sessions.Aside) string {
	if aside.Bang {
		return "/btw!"
	}
	return "/btw"
}

func asideOutcome(aside sessions.Aside) string {
	if aside.Answer == "" {
		return "(" + aside.Ending + ")"
	}
	return shortLine(aside.Answer, btwListAnswerCut)
}

// shortLine is text on one line, cut to length characters.
func shortLine(text string, length int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > length {
		return string(runes[:length-1]) + "…"
	}
	return text
}
