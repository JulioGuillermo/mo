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

// Verdict is what the rules say about one command, from the weakest: the
// stronger of two wins.
type Verdict int

const (
	Unknown Verdict = iota
	// Allowed runs without asking.
	Allowed
	// Excepted is a command an exception (!rule) takes out of the allowed
	// ones: the mode decides, as for one no rule names.
	Excepted
	// Denied (!!rule) asks in every mode, also in all.
	Denied
	// Blocked (!!!rule) never runs.
	Blocked
)

const regexPrefix = "re:"

// levelPrefixes mark what a rule does, longest first: never run, always
// ask, take out of the allowed ones; a rule without one allows.
var levelPrefixes = []struct {
	prefix string
	level  Verdict
}{{"!!!", Blocked}, {"!!", Denied}, {"!", Excepted}}

type rule struct {
	text    string
	level   Verdict
	pattern *regexp.Regexp
	// powershell is the pattern as PowerShell commands are matched: in any
	// case, an alias first as its cmdlet.
	powershell *regexp.Regexp
}

// Compile checks a rule. A rule is a command where * stands for anything:
// alone it is any words, or none ("ls *" also takes "ls"), inside a word any
// characters of it ("git log*"). "re:" starts a regular expression instead.
// Fetches are written "web_fetch <url>", or "fetch <url>" as before the tool
// was renamed: a rule of either kind decides for both. Before it, ! makes an
// exception to the allowed ones, !! asks always and !!! never lets it run.
func Compile(text string) error {
	_, err := compile(text)
	return err
}

func compile(text string) (rule, error) {
	text = strings.TrimSpace(text)
	level, body := Allowed, text
	for _, marked := range levelPrefixes {
		if rest, found := strings.CutPrefix(text, marked.prefix); found {
			level, body = marked.level, strings.TrimSpace(rest)
			break
		}
	}
	if body == "" {
		return rule{}, fmt.Errorf("rule %q: write a command after the !", text)
	}
	compiled, err := compilePattern(body)
	if err != nil {
		return rule{}, fmt.Errorf("rule %q: %w", text, err)
	}
	anyCase, err := compilePattern(powershellRule(body))
	if err != nil {
		return rule{}, fmt.Errorf("rule %q: %w", text, err)
	}
	anyCase = regexp.MustCompile("(?i)" + anyCase.String())
	return rule{text: text, level: level, pattern: compiled, powershell: anyCase}, nil
}

// compileAtLeast compiles a rule that does at least what level does, as
// those of a deny list, which ask unless they block.
func compileAtLeast(text string, level Verdict) (rule, error) {
	compiled, err := compile(text)
	compiled.level = max(compiled.level, level)
	return compiled, err
}

func compilePattern(text string) (*regexp.Regexp, error) {
	if expression, isRegex := strings.CutPrefix(text, regexPrefix); isRegex {
		return regexp.Compile(strings.TrimSpace(expression))
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
	return regexp.MustCompile(expression.String()), nil
}

// Policy is the mode and rules in force. The UI changes it while the agent
// reads it, so it is safe to share.
type Policy struct {
	mu    sync.Mutex
	mode  Mode
	rules []rule
	// within is the policy a narrowed one never goes past: its mode when
	// stricter, its rules, and the rules added to it later.
	within *Policy
}

// Narrowed is a policy never looser than p: the stricter of p's mode and
// mode, which may be empty, and p's rules with deny added. It follows the
// changes to p; rules allowed through it are added to p.
func (p *Policy) Narrowed(mode Mode, deny []string) (*Policy, error) {
	if mode != "" && !mode.Valid() {
		return nil, fmt.Errorf("permissions %q: use ask, edits, auto or all", mode)
	}
	narrowed := &Policy{mode: mode, within: p}
	for _, text := range deny {
		compiled, err := compileAtLeast(text, Denied)
		if err != nil {
			return nil, err
		}
		narrowed.rules = append(narrowed.rules, compiled)
	}
	return narrowed, nil
}

// New builds a policy, refusing a rule that does not compile. The rules of
// allow do what their prefix says; those of deny ask at least.
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
		compiled, err := compileAtLeast(text, Denied)
		if err != nil {
			return nil, err
		}
		policy.rules = append(policy.rules, compiled)
	}
	return policy, nil
}

func (p *Policy) Mode() Mode {
	p.mu.Lock()
	mode := p.mode
	p.mu.Unlock()
	if p.within != nil {
		return Stricter(p.within.Mode(), mode)
	}
	return mode
}

func (p *Policy) SetMode(mode Mode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
}

// Allow adds a rule from now on: one that lets a command through, or with
// a prefix, one that keeps it from doing so.
func (p *Policy) Allow(text string) error {
	if p.within != nil {
		return p.within.Allow(text)
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	compiled, err := compile(text)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = append(p.rules, compiled)
	return nil
}

// Check returns what the rules say about a command of bash, or a fetch,
// as CheckCommand does.
func (p *Policy) Check(command string) (Verdict, string) {
	return p.CheckCommand(Command{Text: command})
}

// CheckCommand returns what the rules say about a command and the rule
// that says it: the strongest that matches, so !!! blocking wins over !!
// asking, over a ! exception, over an allowing rule. A PowerShell command
// matches in any case, with aliases read as their cmdlets.
func (p *Policy) CheckCommand(command Command) (Verdict, string) {
	text := strings.Join(strings.Fields(command.Text), " ")
	// A fetch is also written as it was before the tool was renamed, so the
	// rules of that time, a deny among them, still apply.
	forms := []string{text}
	if url, fetch := strings.CutPrefix(text, "web_fetch "); fetch {
		forms = append(forms, "fetch "+url)
	}
	p.mu.Lock()
	verdict, decided := Unknown, ""
	for _, each := range p.rules {
		pattern := each.pattern
		if command.PowerShell {
			pattern = each.powershell
		}
		if each.level > verdict && matchesAny(pattern, forms) {
			verdict, decided = each.level, each.text
		}
	}
	p.mu.Unlock()
	if p.within != nil {
		if within, rule := p.within.CheckCommand(command); within > verdict {
			return within, rule
		}
	}
	return verdict, decided
}

func matchesAny(pattern *regexp.Regexp, forms []string) bool {
	for _, form := range forms {
		if pattern.MatchString(form) {
			return true
		}
	}
	return false
}
