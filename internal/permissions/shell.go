package permissions

import (
	"bytes"
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Command is one simple command of a shell script, its words as the shell
// reads them, quotes removed.
type Command struct {
	Text string
	// Source is the command as written in the script, quotes and
	// redirections kept, to show.
	Source string
	// Writes is set when the command sends its output to a file: such a
	// command is never let through by a rule alone.
	Writes bool
}

// Commands splits a script into its simple commands: those joined with
// pipes, && and ;, inside loops and conditions, and inside $(...) and
// backquotes. A script that does not parse gives an error, and asks.
func Commands(script string) ([]Command, error) {
	file, err := syntax.NewParser().Parse(strings.NewReader(script), "")
	if err != nil {
		return nil, err
	}
	var commands []Command
	syntax.Walk(file, func(node syntax.Node) bool {
		statement, ok := node.(*syntax.Stmt)
		if !ok {
			return true
		}
		var words []string
		switch command := statement.Cmd.(type) {
		case *syntax.CallExpr:
			// bash -c 'script' runs that script: its commands are the
			// parts, when it is written out, so nothing changes it.
			if inner, ok := shellScript(command); ok && len(command.Assigns) == 0 && !writesFile(statement.Redirs) {
				parts, err := Commands(inner)
				if err == nil {
					commands = append(commands, parts...)
					return true
				}
			}
			for _, assign := range command.Assigns {
				words = append(words, assignText(assign))
			}
			for _, word := range command.Args {
				words = append(words, wordText(word))
			}
		case *syntax.DeclClause:
			words = append(words, command.Variant.Value)
			for _, assign := range command.Args {
				words = append(words, assignText(assign))
			}
		}
		if len(words) > 0 {
			written := strings.Join(words, " ")
			// As written, unless it is long and holds commands of its own:
			// those are parts of their own, and it shows $(…) in their place.
			if start, end := int(statement.Pos().Offset()), int(statement.End().Offset()); start >= 0 && end <= len(script) && start < end {
				if as := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(script[start:end]), ";")); !holdsCommands(statement) || (len(as) <= shortSource && !strings.Contains(as, "\n")) {
					written = as
				}
			}
			commands = append(commands, Command{Text: strings.Join(words, " "), Source: written, Writes: writesFile(statement.Redirs)})
		}
		return true
	})
	return commands, nil
}

// writesFile tells whether a redirection sends output to a file other than
// /dev/null; copying one descriptor onto another, as 2>&1, writes nothing.
func writesFile(redirects []*syntax.Redirect) bool {
	for _, redirect := range redirects {
		switch redirect.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll, syntax.RdrInOut, syntax.ClbOut:
			if wordText(redirect.Word) != "/dev/null" {
				return true
			}
		}
	}
	return false
}

// shortSource is how long a part holding commands of its own may be and
// still show as written.
const shortSource = 60

// shells are the shells whose -c runs the script after it.
var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true}

// shellScript is the script a shell's -c runs, as bash -c 'script' or
// sh -ec "script", when it is written out, nothing in it expanded.
func shellScript(command *syntax.CallExpr) (string, bool) {
	if len(command.Args) < 3 {
		return "", false
	}
	name, ok := literalWord(command.Args[0])
	if !ok || !shells[path.Base(name)] {
		return "", false
	}
	for i := 1; i < len(command.Args)-1; i++ {
		flag, ok := literalWord(command.Args[i])
		if !ok || !strings.HasPrefix(flag, "-") || strings.HasPrefix(flag, "--") {
			return "", false
		}
		if strings.HasSuffix(flag, "c") {
			return literalWord(command.Args[i+1])
		}
	}
	return "", false
}

// literalWord is a word's text when nothing in it is expanded.
func literalWord(word *syntax.Word) (string, bool) {
	var text strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			text.WriteString(unescape(part.Value))
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false
			}
			text.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				literal, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}
				text.WriteString(unquote(literal.Value))
			}
		default:
			return "", false
		}
	}
	return text.String(), true
}

// assignText is an assignment as written, the commands in it as $(…).
func assignText(assign *syntax.Assign) string {
	switch {
	case assign.Name == nil:
		return wordText(assign.Value)
	case assign.Value == nil && assign.Array == nil:
		return assign.Name.Value
	case assign.Value == nil:
		return assign.Name.Value + "=" + source(assign.Array)
	}
	return assign.Name.Value + "=" + wordText(assign.Value)
}

// holdsCommands tells whether a statement runs commands inside it, in
// $(...), backquotes or <(...).
func holdsCommands(statement *syntax.Stmt) bool {
	found := false
	syntax.Walk(statement.Cmd, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			found = true
		}
		return !found
	})
	return found
}

// shortWord is a word as written, each command in it as $(…), since it is
// a part of its own.
func shortWord(word *syntax.Word) string {
	var text strings.Builder
	var write func(parts []syntax.WordPart)
	write = func(parts []syntax.WordPart) {
		for _, part := range parts {
			switch part := part.(type) {
			case *syntax.CmdSubst:
				text.WriteString("$(…)")
			case *syntax.ProcSubst:
				text.WriteString("<(…)")
			case *syntax.DblQuoted:
				text.WriteString(`"`)
				write(part.Parts)
				text.WriteString(`"`)
			case *syntax.Lit:
				text.WriteString(part.Value)
			default:
				text.WriteString(source(part))
			}
		}
	}
	write(word.Parts)
	return text.String()
}

// wordText is a word without its quotes when it is plain text, else as
// written, so "$HOME/x" stays "$HOME/x" and no rule mistakes it for a path;
// the commands in it show as $(…).
func wordText(word *syntax.Word) string {
	var text strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			text.WriteString(unescape(part.Value))
		case *syntax.SglQuoted:
			text.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				literal, ok := inner.(*syntax.Lit)
				if !ok {
					return shortWord(word)
				}
				text.WriteString(unquote(literal.Value))
			}
		default:
			return shortWord(word)
		}
	}
	return text.String()
}

// unquote is text inside double quotes as the shell reads it: a backslash
// only escapes $, `, ", \ and a new line there.
func unquote(literal string) string {
	return strings.NewReplacer(`\$`, `$`, "\\`", "`", `\"`, `"`, `\\`, `\`, "\\\n", "").Replace(literal)
}

func unescape(literal string) string {
	var text strings.Builder
	escaped := false
	for _, character := range literal {
		if character == '\\' && !escaped {
			escaped = true
			continue
		}
		escaped = false
		text.WriteRune(character)
	}
	return text.String()
}

func source(node syntax.Node) string {
	var text bytes.Buffer
	_ = syntax.NewPrinter(syntax.Minify(true)).Print(&text, node)
	return strings.TrimSpace(text.String())
}
