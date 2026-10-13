package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const shownSuggestions = 10

type command struct {
	name        string
	argument    string
	description string
}

var builtinCommands = []command{
	{name: "clear", description: "start a new conversation; the last one stays saved"},
	{name: "resume", description: "go back to a saved conversation of this folder"},
	{name: "rewind", description: "go back to one of your prompts to edit it, in a fork (esc esc)"},
	{name: "fork", description: "go on in a copy of the conversation"},
	{name: "compact", description: "summarize the conversation to free context"},
	{name: "settings", description: "model, tools, context, web, loop guard, display"},
	{name: "providers", description: "add, edit or delete providers"},
	{name: "mcp", description: "MCP servers: status, add, edit, delete"},
	{name: "plugins", description: "plugins and their commands"},
	{name: "notify", description: "send a test notification"},
	{name: "view", argument: "<file>", description: "show an image, SVG or Mermaid file"},
	{name: "help", description: "commands and keys"},
	{name: "quit", description: "exit mo"},
}

var keyHelp = [][2]string{
	{"enter", "send"},
	{"ctrl+j, alt+enter", "new line"},
	{"tab, ↑ ↓", "complete a command"},
	{"esc, ctrl+c", "stop the current run"},
	{"pgup, pgdown, wheel", "scroll the conversation"},
	{"shift+tab", "change the permission mode"},
	{"ctrl+o, click", "open or fold reasoning and tool calls"},
	{"ctrl+v", "paste a copied image or files as attachments"},
	{"shift+drag", "select text"},
	{"ctrl+d", "quit"},
}

// commands are mo's own commands, then the plugins' ones.
func (m model) commands() []command {
	commands := builtinCommands
	for _, found := range m.session.Plugins.Commands() {
		commands = append(commands, command{name: found.Name, argument: "[text]", description: found.Description})
	}
	return commands
}

// suggestions are the commands that start with what the user typed after
// "/", while the first word is being written.
func (m model) suggestions() []command {
	typed, isCommand := strings.CutPrefix(m.input.Value(), "/")
	if !isCommand || strings.ContainsAny(typed, " \n") {
		return nil
	}
	var matches []command
	for _, candidate := range m.commands() {
		if strings.HasPrefix(candidate.name, typed) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

// completeCommand writes the chosen command into the input and tells
// whether it can run as it is.
func (m *model) completeCommand(chosen command) bool {
	if chosen.argument != "" {
		m.input.SetValue("/" + chosen.name + " ")
		return false
	}
	m.input.SetValue("/" + chosen.name)
	return true
}

func (m model) suggestionList(matches []command) string {
	selected := m.suggestion % len(matches)
	first := max(0, selected-shownSuggestions+1)
	shown := matches[first:min(len(matches), first+shownSuggestions)]
	width := 0
	for _, candidate := range shown {
		width = max(width, len(candidate.name)+len(candidate.argument)+2)
	}
	var lines []string
	for i, candidate := range shown {
		name := "/" + candidate.name
		if candidate.argument != "" {
			name += " " + candidate.argument
		}
		line := fmt.Sprintf(" %-*s  %s ", width, name, candidate.description)
		if first+i == selected {
			lines = append(lines, selectedSuggestion.Render(line))
			continue
		}
		lines = append(lines, toolStyle.Render(fmt.Sprintf(" %-*s ", width, name))+dimStyle.Render(" "+candidate.description))
	}
	return strings.Join(lines, "\n")
}

var (
	selectedSuggestion = lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1a1b26")).Bold(true)
	helpHeading        = lipgloss.NewStyle().Foreground(warning).Bold(true)
)

// helpView lists the commands and the keys in two aligned tables.
func (m model) helpView() string {
	var out []string
	out = append(out, helpHeading.Render("Commands"))
	for _, candidate := range m.commands() {
		name := "/" + candidate.name
		if candidate.argument != "" {
			name += " " + candidate.argument
		}
		out = append(out, "  "+toolName.Render(fmt.Sprintf("%-18s", name))+dimStyle.Render(candidate.description))
	}
	out = append(out, "", helpHeading.Render("Keys"))
	for _, entry := range keyHelp {
		out = append(out, "  "+keyStyle.Render(fmt.Sprintf("%-22s", entry[0]))+dimStyle.Render(entry[1]))
	}
	return strings.Join(out, "\n")
}
