package tui

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
)

const (
	newServerChoice = "\x00new"
	hiddenValue     = "••••••"
)

// mcpDraft holds what the MCP form edits for one server. Arguments go one
// per line, environment variables as NAME=value and headers as Name: value.
// Saved values show hidden unless they only read the environment.
type mcpDraft struct {
	name         string
	isNew        bool
	command      string
	args         string
	env          string
	url          string
	headers      string
	trust        bool
	remove       bool
	savedEnv     map[string]string
	savedHeaders map[string]string
}

func newMCPDraft(name string, server config.MCPServer) *mcpDraft {
	if name == newServerChoice {
		return &mcpDraft{isNew: true}
	}
	return &mcpDraft{
		name:         name,
		command:      server.Command,
		args:         strings.Join(server.Args, "\n"),
		env:          joinPairs(server.Env, "="),
		url:          server.URL,
		headers:      joinPairs(server.Headers, ": "),
		trust:        server.Trust,
		savedEnv:     server.Env,
		savedHeaders: server.Headers,
	}
}

func (d *mcpDraft) fields(taken map[string]config.MCPServer) []huh.Field {
	var fields []huh.Field
	if d.isNew {
		fields = append(fields, huh.NewInput().Title("Name").Description("its tools are called mcp__name__tool").
			Validate(validNewName(taken)).Value(&d.name))
	}
	fields = append(fields,
		huh.NewInput().Title("Command").Description("a server mo starts, e.g. npx; empty for one reached by URL").Value(&d.command),
		huh.NewText().Title("Arguments").Description("one per line").Lines(3).Value(&d.args),
		huh.NewText().Title("Environment").Description("NAME=value per line; ${NAME} reads mo's environment, "+hiddenValue+" keeps the saved value").Lines(3).Value(&d.env),
		huh.NewInput().Title("URL").Description("a server already running, e.g. http://localhost:3000/mcp").Value(&d.url),
		huh.NewText().Title("Headers").Description("Name: value per line, for the URL").Lines(2).Value(&d.headers),
		huh.NewConfirm().Title("Trust its tools").Description("run them without asking").Value(&d.trust),
	)
	if !d.isNew {
		fields = append(fields, huh.NewConfirm().Title("Delete this server").Value(&d.remove))
	}
	return fields
}

// applyTo writes the server into the user layer, or takes it out.
func (d *mcpDraft) applyTo(layer config.Config) (config.Config, error) {
	name := strings.TrimSpace(d.name)
	if d.remove {
		layer.MCP = withoutEntry(layer.MCP, name)
		return layer, nil
	}
	server := config.MCPServer{
		Command: strings.TrimSpace(d.command),
		Args:    lines(d.args),
		URL:     strings.TrimSpace(d.url),
		Trust:   d.trust,
	}
	if (server.Command == "") == (server.URL == "") {
		return layer, fmt.Errorf("MCP %s: set either a command or a URL", name)
	}
	env, err := splitPairs(d.env, "=", d.savedEnv)
	if err != nil {
		return layer, fmt.Errorf("MCP %s environment: %w", name, err)
	}
	headers, err := splitPairs(d.headers, ":", d.savedHeaders)
	if err != nil {
		return layer, fmt.Errorf("MCP %s headers: %w", name, err)
	}
	server.Env, server.Headers = env, headers
	layer.MCP = withEntry(layer.MCP, name, server)
	return layer, nil
}

// pickMCPServer lists the servers to edit, then opens the chosen one.
func (m model) pickMCPServer(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	servers := msg.layer.MCP
	picked := newServerChoice
	var options []huh.Option[string]
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		options = append(options, huh.NewOption(name, name))
	}
	options = append(options, huh.NewOption("+ add a server", newServerChoice))
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("MCP server to edit").Description("esc leaves them as they are").Options(options...).Value(&picked),
	)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		return m.editMCPServer(msg, picked)
	}
	return m.showSettings(&settingsScreen{form: form, layer: msg.layer, next: next})
}

func (m model) editMCPServer(msg settingsLoadedMsg, name string) (tea.Model, tea.Cmd) {
	draft := newMCPDraft(name, msg.layer.MCP[name])
	title := name
	if draft.isNew {
		title = "New MCP server"
	}
	form := huh.NewForm(huh.NewGroup(draft.fields(msg.layer.MCP)...).Title(title)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: msg.layer, apply: draft.applyTo})
}

func lines(text string) []string {
	var out []string
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// splitPairs reads one name and value per line, split at the first sep. A
// hidden value is the saved one.
func splitPairs(text, sep string, saved map[string]string) (map[string]string, error) {
	pairs := map[string]string{}
	for _, line := range lines(text) {
		name, value, found := strings.Cut(line, sep)
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !found || name == "" {
			return nil, fmt.Errorf("%q: write it as name%svalue", line, sep)
		}
		if value == hiddenValue {
			kept, known := saved[name]
			if !known {
				return nil, fmt.Errorf("%s: type its value", name)
			}
			value = kept
		}
		pairs[name] = value
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return pairs, nil
}

// joinPairs writes one name and value per line, the value hidden unless it
// only reads the environment.
func joinPairs(pairs map[string]string, sep string) string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(pairs)) {
		value := pairs[name]
		if !onlyReadsEnvironment(value) {
			value = hiddenValue
		}
		out = append(out, name+sep+value)
	}
	return strings.Join(out, "\n")
}

var environmentReference = regexp.MustCompile(`^(Bearer )?\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)

func onlyReadsEnvironment(value string) bool {
	return environmentReference.MatchString(value)
}
