package tui

import (
	"encoding/json"
	"fmt"
	"github.com/jg/mo/internal/tools"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	formingLines   = 12
	formingCommand = 8
	callWrapper    = "tool_call"
)

// formingCall is a tool call the model is still writing: its name once it
// came, and its arguments so far, JSON not finished yet; its id, the
// agent's own when the reply gives none, and started once it runs while
// the reply is still written, so its block shows instead.
type formingCall struct {
	id      string
	name    string
	args    string
	started bool
}

// growCall adds a piece of a tool call as it streams in.
func (m *model) growCall(index int, id, name, args string) {
	// A call of the next reply: those of the last all ran.
	if index < len(m.forming) && m.forming[index].started {
		m.forming = nil
	}
	m.forming = slices.Clone(m.forming)
	for len(m.forming) <= index {
		m.forming = append(m.forming, formingCall{id: fmt.Sprintf("call_%d", len(m.forming))})
	}
	if id != "" {
		m.forming[index].id = id
	}
	m.forming[index].name += name
	m.forming[index].args += args
}

// formingStarted stops showing a call being written, or waiting its turn,
// once it runs: by its id, or the first waiting of its name when the
// provider gave none while it streamed.
func (m *model) formingStarted(id, name string) {
	for i := range m.forming {
		if m.forming[i].id == id {
			m.forming = slices.Clone(m.forming)
			m.forming[i].started = true
		}
	}
	if slices.ContainsFunc(m.forming, func(call formingCall) bool { return call.id == id }) {
		return
	}
	if at := slices.IndexFunc(m.forming, func(call formingCall) bool { return !call.started && innerName(call) == name }); at >= 0 {
		m.forming = slices.Clone(m.forming)
		m.forming[at].started = true
	}
}

// partialString reads the string a key holds in JSON that may stop short,
// as far as it has come: escapes decoded, the closing quote not needed.
func partialString(json, key string) (string, bool) {
	at := strings.Index(json, `"`+key+`"`)
	if at < 0 {
		return "", false
	}
	rest := strings.TrimLeft(json[at+len(key)+2:], " \t\r\n")
	if !strings.HasPrefix(rest, ":") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	var out strings.Builder
	for i := 1; i < len(rest); {
		switch c := rest[i]; c {
		case '"':
			return out.String(), true
		case '\\':
			if i+1 >= len(rest) {
				return out.String(), true
			}
			switch rest[i+1] {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
			case 'u':
				if i+6 > len(rest) {
					return out.String(), true
				}
				if code, err := strconv.ParseUint(rest[i+2:i+6], 16, 32); err == nil {
					out.WriteRune(rune(code))
				}
				i += 6
				continue
			default:
				out.WriteByte(rest[i+1])
			}
			i += 2
		default:
			r, size := utf8.DecodeRuneInString(rest[i:])
			out.WriteRune(r)
			i += size
		}
	}
	return out.String(), true
}

// formingView shows the calls being written: the tool's name with a
// spinner as soon as it is known, then what it is about to do as it forms,
// a command, or the last lines of a file being written or edited.
func (m model) formingView() string {
	var parts []string
	for _, call := range m.forming {
		if call.name == "" || call.started {
			continue
		}
		// The calls of a reply run one at a time: while one runs, the
		// later ones wait for it; while the user is asked, for them.
		sign := forming()
		switch {
		case m.asks(call):
			sign = askingSign()
		case m.runningTool != nil && m.runningTool.ID != call.id:
			sign = markQueued
		}
		parts = append(parts, m.formingCallView(innerName(call), call.args, sign))
	}
	return strings.Join(parts, "\n")
}

// innerName is the tool a call names, the one inside tool_call too.
func innerName(call formingCall) string {
	if call.name == callWrapper {
		if inner, found := partialString(call.args, "name"); found && inner != "" {
			return inner
		}
	}
	return call.name
}

// formingCallView is a call being written, its arc turning; one whole that
// waits for the calls before it, dotted and saying so.
func (m model) formingCallView(name, args, sign string) string {
	mark := m.printer.toolHeaderAs(name, json.RawMessage(args), sign)
	header := "\n" + mark
	if sign == markQueued {
		header += dimStyle.Render("  waits for the call before it")
	}
	path, _ := partialString(args, "path")
	if path != "" {
		header = "\n" + mark + " " + path
		if sign == markQueued {
			header += dimStyle.Render("  waits for the call before it")
		}
	}
	switch name {
	case "bash":
		command, found := partialString(args, "command")
		if !found {
			return header
		}
		shown := strings.Split(command, "\n")
		if len(shown) > formingCommand {
			shown = shown[len(shown)-formingCommand:]
		}
		return header + "\n" + m.printer.commandLines(strings.Join(shown, "\n"), true)
	case "write", "edit":
		field := "content"
		if name == "edit" {
			field = "new"
		}
		text, found := partialString(args, field)
		if !found {
			return header
		}
		total := strings.Count(text, "\n") + 1
		verb := map[string]string{"write": "writing", "edit": "editing"}[name]
		header += dimStyle.Render(fmt.Sprintf("  %s · %d lines", verb, total))
		return header + "\n" + m.formingWindow(path, text)
	}
	summary := oneLine(args, max(m.mainWidth()-len(name)-8, 20))
	return header + " " + dimStyle.Render(summary)
}

// formingWindow shows the last lines of a file being written, coloured by
// its language, in a small block that follows what arrives.
func (m model) formingWindow(path, text string) string {
	rows := strings.Split(text, "\n")
	if len(rows) > formingLines {
		rows = rows[len(rows)-formingLines:]
	}
	dark := m.printer == nil || m.printer.isDark()
	colors := changePalette(dark)
	width := max(m.mainWidth()-8, 20)
	block := lipgloss.NewStyle().Background(colors.block)
	var out []string
	for _, line := range highlightLines(strings.Join(rows, "\n"), languageOf(path), width, colors.block, dark) {
		fill := block.Render(strings.Repeat(" ", max(width+1-ansi.StringWidth(line), 0)))
		out = append(out, commandEdge.Render("  ▌")+block.Render(" ")+line+fill)
	}
	return strings.Join(out, "\n")
}

// asks tells whether the dialog open asks about a call: approving it, or
// the questions of an ask.
func (m model) asks(call formingCall) bool {
	if m.dialog == nil {
		return false
	}
	if innerName(call) == tools.AskName {
		return true
	}
	for _, group := range m.dialog.groups {
		for _, request := range group.interaction.Approvals {
			if request.Call.ID == call.id {
				return true
			}
		}
	}
	return false
}
