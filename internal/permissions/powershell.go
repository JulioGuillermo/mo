package permissions

import (
	"errors"
	"slices"
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/powershell"
)

var powershellLanguage = sync.OnceValue(powershell.Language)

// errPowerShellSyntax is a script the parser cannot read in full: it asks.
var errPowerShellSyntax = errors.New("PowerShell that does not parse")

// PowerShellCommands splits a PowerShell script into its commands: those
// of pipelines, chains and statements, inside blocks, $(...) and strings,
// and the script a pwsh -Command, cmd /c or bash -c written out runs.
// Code mo cannot read, as Invoke-Expression's or a .NET method's, is a
// hidden part. A script that does not parse gives an error, and asks.
func PowerShellCommands(script string) ([]Command, error) {
	tree, err := ts.NewParser(powershellLanguage()).Parse([]byte(script))
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	if root.HasError() {
		return nil, errPowerShellSyntax
	}
	walker := &powershellWalker{source: []byte(script)}
	walker.walk(root)
	return walker.commands, walker.err
}

type powershellWalker struct {
	source   []byte
	commands []Command
	err      error
}

func (w *powershellWalker) kind(n *ts.Node) string {
	return n.Type(powershellLanguage())
}

func (w *powershellWalker) text(n *ts.Node) string {
	return strings.TrimSpace(string(w.source[n.StartByte():n.EndByte()]))
}

func (w *powershellWalker) children(n *ts.Node) []*ts.Node {
	var nodes []*ts.Node
	for i := range int(n.ChildCount()) {
		nodes = append(nodes, n.Child(i))
	}
	return nodes
}

// walk finds the commands under n, each inner one as a part of its own.
func (w *powershellWalker) walk(n *ts.Node) {
	switch w.kind(n) {
	case "command":
		w.command(n)
	case "invokation_expression":
		if w.isCall(n) {
			w.hidden(w.text(n))
		}
	case "assignment_expression":
		w.assignment(n)
	}
	for _, child := range w.children(n) {
		w.walk(child)
	}
}

// isCall tells a method call, $x.Y() or [T]::Y(), from a property read.
func (w *powershellWalker) isCall(n *ts.Node) bool {
	return slices.ContainsFunc(w.children(n), func(child *ts.Node) bool { return w.kind(child) == "argument_list" })
}

func (w *powershellWalker) hidden(text string) {
	w.commands = append(w.commands, Command{Text: text, Source: text, Hidden: true, PowerShell: true})
}

// assignment writes a file when it sets a variable of a drive, as
// ${C:\notes.txt} = 'x' does.
func (w *powershellWalker) assignment(n *ts.Node) {
	target := w.children(n)[0]
	name := strings.ToLower(w.text(target))
	if !strings.HasPrefix(name, "${") || !strings.Contains(name, ":") {
		return
	}
	scope, _, _ := strings.Cut(strings.TrimPrefix(name, "${"), ":")
	if !slices.Contains([]string{"env", "global", "script", "local", "private", "using", "variable"}, scope) {
		text := w.text(n)
		w.commands = append(w.commands, Command{Text: text, Source: text, Writes: true, PowerShell: true})
	}
}

// command adds a command: its name, an alias as its cmdlet, and its
// arguments, quotes removed; or the commands of the script it runs.
func (w *powershellWalker) command(n *ts.Node) {
	name, args, writes, ok := w.parts(n)
	if !ok {
		return
	}
	cmdlet := cmdletOf(name)
	lower := strings.ToLower(strings.TrimSuffix(strings.ToLower(cmdlet), ".exe"))
	switch {
	case hiddenCmdlets[strings.ToLower(cmdlet)]:
		w.hidden(w.text(n))
		return
	case !writes && (lower == "pwsh" || lower == "powershell"):
		if w.innerPowerShell(n, args) {
			return
		}
	case !writes && lower == "cmd":
		if w.innerCmd(args) {
			return
		}
	case !writes && shells[lower]:
		if w.innerShell(args) {
			return
		}
	}
	text := strings.TrimSpace(cmdlet + " " + strings.Join(args, " "))
	w.commands = append(w.commands, Command{Text: text, Source: w.text(n), Writes: writes, PowerShell: true})
}

// parts are what a command is made of: the name it runs, its arguments
// and whether it redirects to a file. ok is false for a script block run
// with &, whose commands are parts of their own, and the name is "" for
// one mo cannot read, as & $command.
func (w *powershellWalker) parts(n *ts.Node) (name string, args []string, writes, ok bool) {
	for _, child := range w.children(n) {
		switch w.kind(child) {
		case "command_name":
			name = w.text(child)
		case "command_name_expr":
			inner := w.children(child)
			if len(inner) == 1 && w.kind(inner[0]) == "script_block_expression" {
				return "", nil, false, false
			}
			literal, isLiteral := unquoted(w.text(child))
			if !isLiteral {
				w.hidden(w.text(n))
				return "", nil, false, false
			}
			name = literal
		case "command_elements":
			for _, element := range w.children(child) {
				switch w.kind(element) {
				case "command_argument_sep":
				case "redirection":
					writes = writes || w.redirectsToFile(element)
				default:
					word, isLiteral := unquoted(w.text(element))
					if !isLiteral {
						word = w.text(element)
					}
					args = append(args, word)
				}
			}
		}
	}
	return name, args, writes, name != ""
}

// redirectsToFile tells a redirection that writes a file: > or >> and
// their streams, but not to $null, nor 2>&1 merging two streams.
func (w *powershellWalker) redirectsToFile(n *ts.Node) bool {
	operator, target := false, ""
	for _, child := range w.children(n) {
		switch w.kind(child) {
		case "file_redirection_operator":
			operator = true
		case "redirected_file_name":
			target = strings.ToLower(w.text(child))
		}
	}
	return operator && target != "$null"
}

// innerPowerShell adds the commands of the script pwsh -Command runs,
// written out; an encoded one, or one in a variable, is hidden. It tells
// whether the command was taken care of.
func (w *powershellWalker) innerPowerShell(n *ts.Node, args []string) bool {
	for i, arg := range args {
		parameter := strings.ToLower(strings.TrimPrefix(arg, "-"))
		if !strings.HasPrefix(arg, "-") || parameter == "" {
			continue
		}
		switch {
		case parameter == "e" || parameter == "ec" || (len(parameter) >= 2 && strings.HasPrefix("encodedcommand", parameter)):
			w.hidden(w.text(n))
			return true
		case strings.HasPrefix("command", parameter):
			script := strings.Join(args[i+1:], " ")
			if script == "" || strings.HasPrefix(script, "$") || script == "-" {
				w.hidden(w.text(n))
				return true
			}
			w.inner(PowerShellCommands(script))
			return true
		case strings.HasPrefix("file", parameter):
			return false
		}
	}
	return false
}

// innerCmd adds the commands of the line cmd /c or /k runs, split at &&,
// || and &; one that redirects to a file other than nul writes.
func (w *powershellWalker) innerCmd(args []string) bool {
	for i, arg := range args {
		if switchName := strings.ToLower(arg); switchName != "/c" && switchName != "/k" {
			continue
		}
		line := strings.Join(args[i+1:], " ")
		for _, part := range splitCmdLine(line) {
			lower := strings.ToLower(part)
			writes := strings.Contains(strings.ReplaceAll(strings.ReplaceAll(lower, ">nul", ""), "> nul", ""), ">")
			w.commands = append(w.commands, Command{Text: part, Source: part, Writes: writes, PowerShell: true})
		}
		return true
	}
	return false
}

// innerShell adds the commands of the script bash -c runs, written out.
func (w *powershellWalker) innerShell(args []string) bool {
	if len(args) < 2 || args[0] != "-c" {
		return false
	}
	w.inner(Commands(args[1]))
	return true
}

func (w *powershellWalker) inner(commands []Command, err error) {
	if err != nil && w.err == nil {
		w.err = err
	}
	w.commands = append(w.commands, commands...)
}

// splitCmdLine splits a cmd line at &&, || and &, outside quotes.
func splitCmdLine(line string) []string {
	var parts []string
	var current strings.Builder
	quoted := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			quoted = !quoted
		case !quoted && (c == '&' || (c == '|' && i+1 < len(line) && line[i+1] == '|')):
			if i+1 < len(line) && line[i+1] == c {
				i++
			}
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteByte(c)
	}
	parts = append(parts, strings.TrimSpace(current.String()))
	return slices.DeleteFunc(parts, func(part string) bool { return part == "" })
}

// unquoted is a word without its quotes when nothing in it is expanded:
// '...' always, "..." without $ or a backtick.
func unquoted(word string) (string, bool) {
	switch {
	case len(word) >= 2 && word[0] == '\'' && word[len(word)-1] == '\'':
		return strings.ReplaceAll(word[1:len(word)-1], "''", "'"), true
	case len(word) >= 2 && word[0] == '"' && word[len(word)-1] == '"':
		inside := word[1 : len(word)-1]
		return inside, !strings.ContainsAny(inside, "$`")
	case strings.ContainsAny(word, "$`(@{"):
		return word, false
	}
	return word, true
}
