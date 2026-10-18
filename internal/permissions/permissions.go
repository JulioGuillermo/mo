// Package permissions decides which tool calls run without asking: the mode
// the user picked, and rules naming the shell commands and URLs that are
// always allowed or never run without asking.
package permissions

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Mode is how much runs without asking.
type Mode string

const (
	// ModeAsk asks before anything that changes files or runs a command.
	ModeAsk Mode = "ask"
	// ModeEdits lets file changes inside the working directory through.
	ModeEdits Mode = "edits"
	// ModeAuto also lets through the commands and fetches the model judges
	// safe for the request.
	ModeAuto Mode = "auto"
	// ModeAll asks nothing, except what a deny rule names.
	ModeAll Mode = "all"
)

// Modes are the modes from the strictest, in the order shift+tab goes.
var Modes = []Mode{ModeAsk, ModeEdits, ModeAuto, ModeAll}

// Valid tells whether mode is one of Modes.
func (m Mode) Valid() bool {
	return slices.Contains(Modes, m)
}

// Next is the mode after m, back to the first after the last.
func (m Mode) Next() Mode {
	return Modes[(slices.Index(Modes, m)+1)%len(Modes)]
}

// Stricter returns the stricter of two modes; an unknown one counts as none.
func Stricter(a, b Mode) Mode {
	if !b.Valid() || (a.Valid() && slices.Index(Modes, a) <= slices.Index(Modes, b)) {
		return a
	}
	return b
}

// Verdict is what the rules say about one command.
type Verdict int

const (
	Unknown Verdict = iota
	Allowed
	Denied
)

const regexPrefix = "re:"

type rule struct {
	text    string
	pattern *regexp.Regexp
}

// Compile checks a rule. A rule is a command where * stands for anything:
// alone it is any words, or none ("ls *" also takes "ls"), inside a word any
// characters of it ("git log*"). "re:" starts a regular expression instead.
// Fetches are written "web_fetch <url>", or "fetch <url>" as before the tool
// was renamed: a rule of either kind decides for both.
func Compile(text string) error {
	_, err := compile(text)
	return err
}

func compile(text string) (rule, error) {
	text = strings.TrimSpace(text)
	if expression, isRegex := strings.CutPrefix(text, regexPrefix); isRegex {
		pattern, err := regexp.Compile(strings.TrimSpace(expression))
		if err != nil {
			return rule{}, fmt.Errorf("rule %q: %w", text, err)
		}
		return rule{text: text, pattern: pattern}, nil
	}
	var expression strings.Builder
	expression.WriteString("^")
	for i, word := range strings.Fields(text) {
		switch {
		case word == "*" && i == 0:
			expression.WriteString(`.*`)
		case word == "*":
			expression.WriteString(`(?: .*)?`)
		default:
			if i > 0 {
				expression.WriteString(" ")
			}
			pieces := strings.Split(word, "*")
			for j, piece := range pieces {
				pieces[j] = regexp.QuoteMeta(piece)
			}
			expression.WriteString(strings.Join(pieces, `\S*`))
		}
	}
	expression.WriteString("$")
	return rule{text: text, pattern: regexp.MustCompile(expression.String())}, nil
}

// Policy is the mode and rules in force. The UI changes it while the agent
// reads it, so it is safe to share.
type Policy struct {
	mu    sync.Mutex
	mode  Mode
	allow []rule
	deny  []rule
}

// New builds a policy, refusing a rule that does not compile.
func New(mode Mode, allow, deny []string) (*Policy, error) {
	if !mode.Valid() {
		return nil, fmt.Errorf("permissions.mode %q: use ask, edits, auto or all", mode)
	}
	policy := &Policy{mode: mode}
	for _, text := range allow {
		if err := policy.Allow(text); err != nil {
			return nil, err
		}
	}
	for _, text := range deny {
		compiled, err := compile(text)
		if err != nil {
			return nil, err
		}
		policy.deny = append(policy.deny, compiled)
	}
	return policy, nil
}

func (p *Policy) Mode() Mode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mode
}

func (p *Policy) SetMode(mode Mode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
}

// Allow adds a rule that lets a command through from now on.
func (p *Policy) Allow(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	compiled, err := compile(text)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allow = append(p.allow, compiled)
	return nil
}

// Check returns what the rules say about a command and the rule that says
// it. A deny rule wins over an allow rule.
func (p *Policy) Check(command string) (Verdict, string) {
	command = strings.Join(strings.Fields(command), " ")
	// A fetch is also written as it was before the tool was renamed, so the
	// rules of that time, a deny among them, still apply.
	forms := []string{command}
	if url, fetch := strings.CutPrefix(command, "web_fetch "); fetch {
		forms = append(forms, "fetch "+url)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, denied := range p.deny {
		for _, form := range forms {
			if denied.pattern.MatchString(form) {
				return Denied, denied.text
			}
		}
	}
	for _, allowed := range p.allow {
		for _, form := range forms {
			if allowed.pattern.MatchString(form) {
				return Allowed, allowed.text
			}
		}
	}
	return Unknown, ""
}
