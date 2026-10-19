package tui

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
)

const shownSuggestions = 10

type command struct {
	name        string
	argument    string
	description string
	// more, set on a folder offered as an argument, fills it in for more
	// to be typed instead of running the command.
	more bool
	// mention is a file offered after @, to attach.
	mention bool
	// skill is a skill offered after $, to load with the message.
	skill bool
}

var builtinCommands = []command{
	{name: "new", description: "start a new conversation on a clear screen; the last one stays in /resume"},
	{name: "resume", description: "go back to a saved conversation of this folder"},
	{name: "rewind", description: "go back to one of your prompts to edit it, in a fork (esc esc)"},
	{name: "fork", description: "go on in a copy of the conversation"},
	{name: "delete", description: "delete this conversation and the output of its commands, after asking"},
	{name: "clean", argument: "<date or age>", description: "delete this folder's conversations last used before: /clean 2026-08-01, /clean 2 months"},
	{name: "continue", description: "let the model go on after you stopped it"},
	{name: "model", description: "pick a model of the provider in use, from the list it gives"},
	{name: "effort", argument: "[level]", description: "how hard the model thinks: default for its own, or a level it takes"},
	{name: "login", argument: "[provider]", description: "sign in to a provider that uses OAuth, the one in use by default"},
	{name: "logout", argument: "[provider]", description: "forget a provider's sign-in"},
	{name: "title", description: "name the conversation: /title <name>, or /title auto for the model to"},
	{name: "cache", argument: "clear", description: "llama-server: erase what every slot holds, its KV cache"},
	{name: "compact", argument: "[instruction]", description: "summarize the conversation to free context; an instruction is added under the summary prompt, as yours"},
	{name: "skill", argument: "[name] [text]", description: "load a skill with your message, as $name does; no name lists them"},
	{name: "git", argument: "show | compare <branch>", description: "see the changes not committed, or what this branch adds over another as a pull request: their files in a tree, each file's diff beside it"},
	{name: "peers", argument: "[name|incoming|outgoing|on|off]", description: "the other mo windows the model can message, when peers are on; and how this session takes their messages"},
	{name: "settings", description: "global settings, and what this project overrides · /settings project"},
	{name: "reload", description: "load the settings files again, provider, model, effort and permission mode included"},
	{name: "providers", description: "add, edit or delete providers"},
	{name: "mcp", description: "MCP servers: status, add, edit, delete"},
	{name: "plugins", description: "plugins and their commands"},
	{name: "notify", description: "send a test notification"},
	{name: "sidebar", description: "show or hide the side panel of tasks and jobs"},
	{name: "vim", description: "vim keys on or off for this session; render.keys in /settings keeps it"},
	{name: "jobs", argument: "[tell]", description: "background jobs; tell: let the model hear the ones that ended now"},
	{name: "kill", argument: "<n>", description: "stop a background job"},
	{name: "agents", argument: "[n]", description: "subagents: pick one, or n, to see what it does and talk to it"},
	{name: "diagrams", description: "show every diagram as its code, or drawn again"},
	{name: "view", argument: "[file]", description: "show a file to you only: images, SVG and Mermaid here, text and code in a viewer; @name, a *.go or **/*name* pattern search for it, no argument or a folder opens a tree of its files"},
	{name: "help", description: "commands and keys"},
	{name: "quit", description: "exit mo"},
}

var keyHelp = [][2]string{
	{"enter", "send"},
	{"shift+enter, ctrl+j, alt+enter", "new line"},
	{"tab, ↑ ↓", "complete a command"},
	{"esc, ctrl+c", "stop the current run"},
	{"ctrl+x", "clear what is typed; the run goes on"},
	{"↑ with nothing typed", "bring back the messages waiting for the model, to edit them"},
	{"ctrl+b", "send the command that blocks the model to the background"},
	{"vim keys", "esc normal · i a A I o insert · ctrl+h/j/k/l panes · j k gg G ctrl+d ctrl+u move · l h open and fold · v y select and copy · dd x D w b 0 $ edit · : command"},
	{"pgup, pgdown, wheel, ctrl+↑↓", "scroll the conversation; with nothing typed, ctrl+j k too"},
	{"ctrl+←→", "with nothing typed, move the wide diagrams across; ctrl+h l too"},
	{"right click on a diagram", "its code, the drawing, whole, copy; /diagrams for all"},
	{"shift+tab", "change the permission mode"},
	{"ctrl+o, click", "open or fold reasoning and tool calls"},
	{"ctrl+v", "paste a copied image or files as attachments"},
	{"drag", "select text and copy it"},
	{"click", "buttons, options, tabs, commands, the mode, the model, a job"},
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
// "/", while the first word is being written; after /login or /logout and a
// space, the providers it takes.
func (m model) suggestions() []command {
	// A subagent's input is a message for it: no commands.
	if m.lensOf != 0 {
		return nil
	}
	if m.suggestionsClosed && m.suggestionsClosedOn == m.input.Value() {
		return nil
	}
	if typed, ok := m.mention(); ok {
		return m.mentionSuggestions(typed)
	}
	if typed, ok := strings.CutPrefix(m.input.Value(), "$"); ok && !strings.ContainsAny(typed, " \n") {
		return m.skillSuggestions(typed, "$")
	}
	typed, isCommand := strings.CutPrefix(m.input.Value(), "/")
	if !isCommand || strings.Contains(typed, "\n") {
		return nil
	}
	if name, partial, spaced := strings.Cut(typed, " "); spaced {
		switch name {
		case "view":
			return m.pathSuggestions(partial)
		case "effort":
			return m.effortSuggestions(partial)
		case "git":
			return m.gitSuggestions(partial)
		case "peers":
			return m.peerSuggestions(partial)
		case "skill":
			if strings.Contains(partial, " ") {
				return nil
			}
			return m.skillSuggestions(partial, "/skill ")
		}
		return m.providerSuggestions(name, partial)
	}
	var matches []command
	for _, candidate := range m.commands() {
		if strings.HasPrefix(candidate.name, typed) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

// providerSuggestions are the providers /login can sign in to, or /logout
// has a sign-in to forget, that start with what was typed. Each is a
// command ready to run, so enter runs it as it is.
func (m model) providerSuggestions(name, partial string) []command {
	if (name != "login" && name != "logout") || strings.Contains(partial, " ") {
		return nil
	}
	var matches []command
	for _, provider := range slices.Sorted(maps.Keys(m.session.Config.Providers)) {
		settings := m.session.Config.Providers[provider]
		if !strings.HasPrefix(provider, partial) || settings.OAuth == nil || settings.OAuth.Flow == config.OAuthClient {
			continue
		}
		_, err := auth.Load(provider)
		signedIn := err == nil
		switch {
		case name == "logout" && !signedIn:
			continue
		case signedIn:
			matches = append(matches, command{name: name + " " + provider, description: "signed in"})
		default:
			matches = append(matches, command{name: name + " " + provider, description: "not signed in"})
		}
	}
	return matches
}

// completeCommand writes the chosen command into the input and tells
// whether it can run as it is.
func (m *model) completeCommand(chosen command) bool {
	if chosen.mention {
		m.completeMention(chosen)
		return false
	}
	if chosen.skill {
		m.input.SetValue(chosen.name + " ")
		m.input.CursorEnd()
		m.suggestion = 0
		return false
	}
	if chosen.more {
		m.input.SetValue("/" + chosen.name)
		m.input.CursorEnd()
		m.suggestion = 0
		return false
	}
	if chosen.argument != "" {
		m.input.SetValue("/" + chosen.name + " ")
		return false
	}
	m.input.SetValue("/" + chosen.name)
	return true
}

func (m model) suggestionList(matches []command) string {
	list, _ := m.suggestionLayout(matches)
	return list
}

// suggestionLayout draws the commands that match and runs the one clicked,
// as enter would.
func (m model) suggestionLayout(matches []command) (string, []region) {
	selected := m.suggestion % len(matches)
	first := max(0, selected-shownSuggestions+1)
	shown := matches[first:min(len(matches), first+shownSuggestions)]
	width := 0
	for _, candidate := range shown {
		width = max(width, len(candidate.name)+len(candidate.argument)+2)
	}
	var lines []string
	var regions []region
	for i, candidate := range shown {
		chosen := candidate
		regions = append(regions, row(i, func(m model) (tea.Model, tea.Cmd) {
			if !m.completeCommand(chosen) || m.settingsInFlight {
				return m, nil
			}
			return m.submit()
		}))
		name := "/" + candidate.name
		switch {
		case candidate.mention:
			name = "@" + candidate.name
		case candidate.skill:
			name = candidate.name
		}
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
	return strings.Join(lines, "\n"), regions
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

// maxPathSuggestions is how many files and folders tab offers at once.
const maxPathSuggestions = 200

// pathSuggestions are the files and folders after /view that start with
// what was typed, folders first; hidden ones only when a dot is typed.
func (m model) pathSuggestions(partial string) []command {
	if paths, search := m.viewTargets(partial); search {
		return viewMatches(paths)
	}
	dir, prefix := filepath.Split(partial)
	base := dir
	if !filepath.IsAbs(base) {
		base = filepath.Join(m.session.WorkDir, dir)
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, dir[2:])
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var folders, files []command
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		full := "view " + dir + name
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(base, name)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			folders = append(folders, command{name: full + "/", description: "folder", more: true})
			continue
		}
		files = append(files, command{name: full, description: "file"})
	}
	found := append(folders, files...)
	if len(found) == 0 && partial != "" && !strings.ContainsAny(partial, `/\`) {
		// No path starts so: the project's files with that in their name.
		return viewMatches(fuzzyFiles(m.files.list(m.session.WorkDir), partial))
	}
	return found[:min(len(found), maxPathSuggestions)]
}
