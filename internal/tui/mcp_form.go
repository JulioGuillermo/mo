package tui

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/tools"
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
	// switches shows whether the server is on and its tools' mode as
	// fields, as /mcp opens it; the settings menu has them under Tools.
	switches bool
	on       bool
	mode     string
	// defaultMode is the tool mode the server follows when it has none.
	defaultMode string
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
	if d.switches {
		entries = append(entries, entry{"On", func() string { return onOff(d.on) }, func() huh.Field {
			return huh.NewConfirm().Title("On").Description("off: not started, its tools are gone; set in tools.mcp_disabled").Affirmative("On").Negative("Off").Value(&d.on)
		}}, entry{"Tool mode", func() string { return orDefault(d.mode, d.defaultMode+" (default)") }, func() huh.Field {
			return huh.NewSelect[string]().Title("Tool mode").Description("how its tools reach the model; set in tools.mcp_modes").Options(
				huh.NewOption("as the default: "+d.defaultMode, ""),
				huh.NewOption("full: declared with its description", string(tools.ModeFull)),
				huh.NewOption("compact: declared without descriptions", string(tools.ModeCompact)),
				huh.NewOption("search: found when looked for", string(tools.ModeSearch)),
				huh.NewOption("group: one entry for the server, all its tools when looked for", string(tools.ModeGroup)),
			).Value(&d.mode)
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
		layer.Tools.MCPDisabled = switchedOn(layer.Tools.MCPDisabled, name, true)
		layer.Tools.MCPModes = withoutEntry(layer.Tools.MCPModes, name)
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
	if d.switches {
		layer = d.switchesTo(layer, false)
	}
	return layer, nil
}

// switchesTo writes whether the server is on and its mode into a layer;
// onlySet leaves alone what the layer does not set, as a project's file,
// whose list of servers off, once set, replaces the user's.
func (d *mcpDraft) switchesTo(layer config.Config, onlySet bool) config.Config {
	name := strings.TrimSpace(d.name)
	if !onlySet || layer.Tools.MCPDisabled != nil {
		layer.Tools.MCPDisabled = switchedOn(layer.Tools.MCPDisabled, name, d.on || d.remove)
		if onlySet && layer.Tools.MCPDisabled == nil {
			layer.Tools.MCPDisabled = []string{}
		}
	}
	if _, set := layer.Tools.MCPModes[name]; !onlySet || set {
		if d.mode == "" || d.remove {
			layer.Tools.MCPModes = withoutEntry(layer.Tools.MCPModes, name)
		} else {
			layer.Tools.MCPModes = withEntry(layer.Tools.MCPModes, name, tools.Mode(d.mode))
		}
	}
	return layer
}

// projectSwitches is the project's file with the server's switches, when
// the project sets them and so would hide a change to the user file.
func (d *mcpDraft) projectSwitches(msg settingsLoadedMsg) *config.Config {
	if !d.switches || msg.projectErr != nil || !msg.project.Trusted {
		return nil
	}
	before := msg.project.Layer
	after := d.switchesTo(before, true)
	if slices.Equal(after.Tools.MCPDisabled, before.Tools.MCPDisabled) && maps.Equal(after.Tools.MCPModes, before.Tools.MCPModes) {
		return nil
	}
	return &after
}

// switchedOn is the list of servers off with name taken out when on, or
// put in.
func switchedOn(off []string, name string, on bool) []string {
	off = slices.DeleteFunc(slices.Clone(off), func(each string) bool { return each == name })
	if !on {
		off = append(off, name)
		slices.Sort(off)
	}
	if len(off) == 0 {
		return nil
	}
	return off
}

// pickMCPServer lists the servers to edit, then opens the chosen one, the
// same way pickProvider does.
func (m model) pickMCPServer(msg settingsLoadedMsg, menu *settingsMenu) (tea.Model, tea.Cmd) {
	servers := msg.layer.MCP
	picked := newServerChoice
	var options []huh.Option[string]
	width := 0
	for name := range servers {
		width = max(width, len(name))
	}
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		options = append(options, huh.NewOption(fmt.Sprintf("%-*s   %s", width, name, m.mcpState(name)), name))
	}
	options = append(options, huh.NewOption("+ add a server", newServerChoice))
	form := newForm(huh.NewGroup(
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
	if menu == nil {
		live := m.session.Config.Tools
		draft.switches = true
		draft.on = !slices.Contains(live.MCPDisabled, name)
		draft.mode = string(live.MCPModes[name])
		draft.defaultMode = orDefault(string(live.Mode), string(tools.ModeFull))
	}
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
		return m.saveServer(e, msg, menu, what, draft.forKind())
	}
	e.actions = []editorAction{{label: mcpTestLabel, run: func(m model) (tea.Model, tea.Cmd) { return m.testMCP(e, draft) }}}
	if !draft.isNew {
		e.removeLabel = "Delete this server"
		e.remove = func(m model) (tea.Model, tea.Cmd) {
			gone := *draft
			gone.remove = true
			return m.saveServer(e, msg, menu, what, &gone)
		}
	}
	return m.showEditor(e, "")
}

// saveServer saves the server; from /mcp, also into the project's file
// when that sets whether it is on or its mode.
func (m model) saveServer(e *editor, msg settingsLoadedMsg, menu *settingsMenu, what string, draft *mcpDraft) (tea.Model, tea.Cmd) {
	project := draft.projectSwitches(msg)
	if menu != nil || project == nil {
		return m.saveEdit(e, msg, menu, what, draft.applyTo)
	}
	if _, err := draft.applyTo(msg.layer); err != nil {
		e.notice = errorStyle.Render("not saved: " + err.Error())
		return m.showEditor(e, editorSave)
	}
	return m.saveOrWait(&settingsScreen{layer: msg.layer, apply: draft.applyTo, project: project})
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

const (
	mcpTestLabel  = "Test connection"
	mcpTestChoice = editorActionPrefix + "0"
)

// mcpTestedMsg is how a test of a server's connection went, for its editor.
type mcpTestedMsg struct {
	editor *editor
	tools  []string
	err    error
}

// testMCP connects to the server as the editor has it, saved or not, and
// says in it how that went.
func (m model) testMCP(e *editor, draft *mcpDraft) (tea.Model, tea.Cmd) {
	name := orDefault(strings.TrimSpace(draft.name), "test")
	tried := *draft.forKind()
	tried.name = name
	layer, err := tried.applyTo(config.Config{})
	if err != nil {
		e.notice = errorStyle.Render("✗ " + err.Error())
		return m.showEditor(e, mcpTestChoice)
	}
	server := layer.MCP[name]
	e.notice = dimStyle.Render("testing the connection…")
	shown, cmd := m.showEditor(e, mcpTestChoice)
	return shown, tea.Batch(cmd, func() tea.Msg {
		tools, err := mcphub.Test(context.Background(), name, server, config.LogDir())
		return mcpTestedMsg{editor: e, tools: tools, err: err}
	})
}

// mcpTested says in the server's editor how its test went, at once when it
// is open, else when it is opened again.
func (m model) mcpTested(msg mcpTestedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		msg.editor.notice = errorStyle.Render("✗ " + oneLine(msg.err.Error(), 200))
	} else {
		msg.editor.notice = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a")).Render(
			fmt.Sprintf("✓ connected · %d tools", len(msg.tools))) + dimStyle.Render(testedTools(msg.tools))
	}
	if m.settings != nil && m.settings.editor == msg.editor {
		return m.showEditor(msg.editor, mcpTestChoice)
	}
	return m, nil
}

// testedTools are the first tools a server offers, to recognise it by.
func testedTools(names []string) string {
	if len(names) == 0 {
		return ""
	}
	shown := names[:min(len(names), 6)]
	text := ": " + strings.Join(shown, ", ")
	if len(names) > len(shown) {
		text += ", …"
	}
	return text
}

// mcpState is how a server is doing in this session: off, its tools and
// their mode, or why it failed.
func (m model) mcpState(name string) string {
	live := m.session.Config.Tools
	if slices.Contains(live.MCPDisabled, name) {
		return dimStyle.Render("○ off")
	}
	for _, status := range m.session.MCP {
		if status.Name != name {
			continue
		}
		if status.Err != nil {
			return errorStyle.Render("✗ " + oneLine(status.Err.Error(), 50))
		}
		mode := string(live.MCPModes[name])
		if mode != "" {
			mode = " · " + mode
		}
		return dimStyle.Render(fmt.Sprintf("● %d tools%s", status.Tools, mode))
	}
	return dimStyle.Render("· not started yet")
}
