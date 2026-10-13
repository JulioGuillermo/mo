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
	kindCommand     = "command"
	kindURL         = "url"
	newServerChoice = "\x00new"
	hiddenValue     = "••••••"
)

// mcpDraft holds what the MCP form edits for one server. Arguments go one
// per line, environment variables as NAME=value and headers as Name: value.
// Saved values show hidden unless they only read the environment.
type mcpDraft struct {
	name  string
	isNew bool
	// kind is how mo reaches the server: a command it starts, or a URL.
	kind         string
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
		return &mcpDraft{isNew: true, kind: kindCommand}
	}
	kind := kindCommand
	if server.URL != "" {
		kind = kindURL
	}
	return &mcpDraft{
		name:         name,
		kind:         kind,
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

// entries are the fields of the server's kind: a command to start, with
// its arguments and environment, or a URL with its headers.
func (d *mcpDraft) entries(taken map[string]config.MCPServer) []entry {
	var entries []entry
	if d.isNew {
		entries = append(entries, entry{"Name", func() string { return valueOr(d.name, "required") }, func() huh.Field {
			return huh.NewInput().Title("Name").Description("its tools are called mcp__name__tool").Validate(validNewName(taken)).Value(&d.name)
		}})
	}
	entries = append(entries, entry{"Type", func() string { return kindLabels[d.kind] }, func() huh.Field {
		return huh.NewSelect[string]().Title("Type").Options(
			huh.NewOption(kindLabels[kindCommand]+": mo starts it, e.g. npx or uvx", kindCommand),
			huh.NewOption(kindLabels[kindURL]+": a server already running, reached over HTTP", kindURL),
		).Value(&d.kind)
	}})
	if d.kind == kindURL {
		entries = append(entries,
			entry{"URL", func() string { return valueOr(d.url, "required") }, func() huh.Field {
				return huh.NewInput().Title("URL").Description("e.g. http://localhost:3000/mcp").Value(&d.url)
			}},
			entry{"Headers", func() string { return countOf(d.headers, "header", "headers") }, func() huh.Field {
				return huh.NewText().Title("Headers").Description("Name: value per line; " + hiddenValue + " keeps the saved value").Lines(6).Value(&d.headers)
			}},
		)
	} else {
		entries = append(entries,
			entry{"Command", func() string { return valueOr(d.command, "required") }, func() huh.Field {
				return huh.NewInput().Title("Command").Description("e.g. npx").Value(&d.command)
			}},
			entry{"Arguments", func() string { return countOf(d.args, "argument", "arguments") }, func() huh.Field {
				return huh.NewText().Title("Arguments").Description("one per line, e.g. -y then @modelcontextprotocol/server-github").Lines(6).Value(&d.args)
			}},
			entry{"Environment", func() string { return countOf(d.env, "variable", "variables") }, func() huh.Field {
				return huh.NewText().Title("Environment").Description("NAME=value per line; ${NAME} reads mo's environment, " + hiddenValue + " keeps the saved value").Lines(6).Value(&d.env)
			}},
		)
	}
	return append(entries, entry{"Trust its tools", func() string { return yesNo(d.trust) }, func() huh.Field {
		return huh.NewConfirm().Title("Trust its tools").Description("run them without asking").Value(&d.trust)
	}})
}

// forKind keeps only the fields of the draft's kind, so a server switched
// from a command to a URL does not keep both.
func (d mcpDraft) forKind() *mcpDraft {
	if d.kind == kindURL {
		d.command, d.args, d.env = "", "", ""
	} else {
		d.url, d.headers = "", ""
	}
	return &d
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

// pickMCPServer lists the servers to edit, then opens the chosen one, the
// same way pickProvider does.
func (m model) pickMCPServer(msg settingsLoadedMsg, menu *settingsMenu) (tea.Model, tea.Cmd) {
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
		return m.editMCPServer(msg, picked, menu)
	}
	screen := &settingsScreen{form: form, layer: msg.layer, next: next, help: menuHelp}
	if menu != nil {
		screen.back = func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	}
	return m.showSettings(screen)
}

var kindLabels = map[string]string{kindCommand: "command", kindURL: "URL"}

// editMCPServer fills in a server a field at a time. Saved from the
// settings menu it waits there for Save and close; from /mcp it is saved
// at once.
func (m model) editMCPServer(msg settingsLoadedMsg, name string, menu *settingsMenu) (tea.Model, tea.Cmd) {
	draft := newMCPDraft(name, msg.layer.MCP[name])
	title := "MCP server " + name
	if draft.isNew {
		title = "New MCP server"
	}
	e := &editor{
		title:     title,
		entries:   func() []entry { return draft.entries(msg.layer.MCP) },
		saveLabel: "Save",
		back:      func(m model) (tea.Model, tea.Cmd) { return m.pickMCPServer(msg, menu) },
		snapshot: func() func() {
			saved := *draft
			return func() { *draft = saved }
		},
	}
	what := "MCP server " + orDefault(name, "new")
	e.save = func(m model) (tea.Model, tea.Cmd) {
		if draft.isNew {
			if err := validNewName(msg.layer.MCP)(draft.name); err != nil {
				e.notice = errorStyle.Render("not saved: " + err.Error())
				return m.showEditor(e, "Name")
			}
		}
		return m.saveEdit(e, msg, menu, what, draft.forKind().applyTo)
	}
	if !draft.isNew {
		e.removeLabel = "Delete this server"
		e.remove = func(m model) (tea.Model, tea.Cmd) {
			gone := *draft
			gone.remove = true
			return m.saveEdit(e, msg, menu, what, gone.applyTo)
		}
	}
	return m.showEditor(e, "")
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
