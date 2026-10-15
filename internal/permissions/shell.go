package permissions

import (
	"bytes"
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
			for _, assign := range command.Assigns {
				words = append(words, source(assign))
			}
			for _, word := range command.Args {
				words = append(words, wordText(word))
			}
		case *syntax.DeclClause:
			words = append(words, source(command))
		}
		if len(words) > 0 {
			written := strings.Join(words, " ")
			if start, end := int(statement.Pos().Offset()), int(statement.End().Offset()); start >= 0 && end <= len(script) && start < end {
				written = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(script[start:end]), ";"))
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

// wordText is a word without its quotes when it is plain text, else as
// written, so "$HOME/x" stays "$HOME/x" and no rule mistakes it for a path.
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
					return source(word)
				}
				text.WriteString(literal.Value)
			}
		default:
			return source(word)
		}
	}
	return text.String()
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
