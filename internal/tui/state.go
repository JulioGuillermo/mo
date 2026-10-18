package tui

import (
	"fmt"
	"image/color"
	"path"
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/tools"
)

// state is what the agent is doing now, in a word, for the spinner and the
// status line: reasoning, writing its answer, running a tool that reads or
// one that changes something, compacting, or waiting.
type state struct {
	word, detail string
	tint         color.Color
}

type stateColors struct {
	reasoning, writing, reading, changing, compacting, waiting, yours, prompt color.Color
}

func statePalette(dark bool) stateColors {
	if dark {
		return stateColors{
			reasoning: accent, writing: warning, reading: lipgloss.Color("#9ece6a"), changing: lipgloss.Color("#f7768e"),
			compacting: lipgloss.Color("#bb9af7"), waiting: lipgloss.Color("#737aa2"), yours: lipgloss.Color("#7dcfff"),
			prompt: lipgloss.Color("#73daca"),
		}
	}
	return stateColors{
		reasoning: lipgloss.Color("#2e7de9"), writing: lipgloss.Color("#8c6c3e"), reading: lipgloss.Color("#587539"), changing: lipgloss.Color("#f52a65"),
		compacting: lipgloss.Color("#9854f1"), waiting: lipgloss.Color("#8990b3"), yours: lipgloss.Color("#007197"),
		prompt: lipgloss.Color("#118c74"),
	}
}

// currentState works out what the agent is doing from what it last sent.
func (m model) currentState() state {
	colors := statePalette(m.printer == nil || m.printer.isDark())
	switch {
	case m.dialog != nil:
		return state{word: "your turn", detail: "answer above", tint: colors.yours}
	case m.waitingFor != "":
		return state{word: "waiting", detail: m.waitingFor, tint: colors.waiting}
	case m.activity != "":
		if m.activity == agent.CompactingActivity {
			return state{word: "compacting", tint: colors.compacting}
		}
		return state{word: "checking", detail: m.activity, tint: colors.compacting}
	case m.writingNow() != nil:
		// A call being written: the model is at it still.
		writing := m.writingNow()
		return m.toolState(writing.name, writing.args, colors)
	case m.runningTool != nil:
		return m.toolState(m.runningTool.Name, string(m.runningTool.Args), colors)
	case m.waitingForTools():
		// The model is done writing: it waits for its calls, not at the last.
		return state{word: "waiting", detail: "for the tools", tint: colors.waiting}
	case m.lastEvent == llm.EventPromptProgress && m.promptRead.Total > 0:
		return state{word: "prompt", detail: promptDetail(m.promptRead), tint: colors.prompt}
	case m.lastEvent == llm.EventReasoning && m.reasoning != "":
		return state{word: "reasoning", tint: colors.reasoning}
	case m.lastEvent == llm.EventText && m.stream != "":
		return state{word: "talking", tint: colors.writing}
	}
	return state{word: "waiting", detail: "for the model", tint: colors.waiting}
}

// toolState says whether a tool reads or changes something: changing is
// red, as are bash and anything the registry counts as a change.
func (m model) toolState(name, args string, colors stateColors) state {
	call := llm.ToolCall{Name: name, Args: []byte(args)}
	call = m.unwrap(call)
	call.Name = tools.Canonical(call.Name)
	changes := alwaysChanging[call.Name] || (call.Name == "bash" && tools.BashChanges(call.Args))
	if m.session.Agent != nil && m.session.Agent.Tools != nil {
		changes = changes || m.session.Agent.Tools.Changes(call) || m.session.Agent.Tools.Warning(call) != ""
	}
	tint, word := colors.reading, "reading"
	if changes {
		tint, word = colors.changing, "changing"
	}
	if verb, ok := toolVerbs[call.Name]; ok {
		word = verb
	}
	args = string(call.Args)
	field := func(name string) string {
		value, _ := partialString(args, name)
		return oneLine(value, stateDetailWidth)
	}
	detail := call.Name
	switch call.Name {
	case "read", "write", "edit", "ls", "mkdir", "rm":
		detail = orDefault(field("path"), call.Name)
	case "grep":
		detail = field("pattern")
	case "web_fetch":
		detail = field("url")
	case "web_search":
		detail = field("query")
	case "git":
		word, detail = "versioning", field("action")
	case tools.AgentName:
		detail = field("name")
	case "bash":
		command, _ := partialString(args, "command")
		detail = programs(command)
	}
	if server, tool, ok := mcpTool(call.Name); ok {
		word, detail = "calling", server+" › "+tool
	}
	return state{word: word, detail: detail, tint: tint}
}

// stateDetailWidth is how much of a call's detail the status line shows.
const stateDetailWidth = 40

// toolVerbs are what the status line says each tool does; the others read
// or change, as they do.
var toolVerbs = map[string]string{
	"read": "reading", "grep": "scanning", "ls": "listing", "write": "writing", "edit": "editing",
	"rm": "removing", "mkdir": "creating", "bash": "running", "web_fetch": "fetching", "web_search": "searching",
	"memory": "remembering", "todo": "planning", tools.AgentName: "delegating", "ui": "drawing",
}

// wrapper is a program that runs another one and says nothing of what the
// command does itself: valued are its flags that take a value, and before is
// how many arguments of its own come before the command, as the duration of
// timeout or the host of ssh. Some need a subcommand to run one, "docker
// exec" or "uv run", and are keyed with it.
type wrapper struct {
	valued []string
	before int
}

var wrappers = map[string]wrapper{
	// Run it as another user, with another priority or limits.
	"sudo": {valued: []string{"-u", "-g", "-C", "-h", "-p", "-r", "-t", "-U", "-T", "-D", "-R", "--user", "--group", "--host", "--prompt",
		"--role", "--type", "--other-user", "--chdir", "--chroot", "--close-from"}},
	"doas":        {valued: []string{"-u", "-C"}},
	"pkexec":      {valued: []string{"--user"}},
	"runuser":     {valued: []string{"-u", "-g", "-G", "--user", "--group"}},
	"nice":        {valued: []string{"-n", "--adjustment"}},
	"ionice":      {valued: []string{"-c", "-n", "-p", "-P", "-u", "--class", "--classdata"}},
	"chrt":        {valued: []string{"-p"}, before: 1},
	"taskset":     {before: 1},
	"numactl":     {valued: []string{"-N", "-m", "-C", "-i", "--cpunodebind", "--membind", "--physcpubind", "--interleave"}},
	"systemd-run": {valued: []string{"-u", "-p", "-E", "-t", "--unit", "--property", "--description", "--setenv", "--slice", "--uid", "--gid", "--working-directory"}},
	// Run it with a limit, in the background, or detached.
	"timeout":  {valued: []string{"-s", "-k", "--signal", "--kill-after"}, before: 1},
	"nohup":    {},
	"setsid":   {},
	"flock":    {before: 1},
	"watch":    {valued: []string{"-n", "--interval"}},
	"faketime": {before: 1},
	// Change what it runs in or sees.
	"env":        {valued: []string{"-u", "-C", "-S", "--unset", "--chdir", "--split-string"}},
	"exec":       {valued: []string{"-a"}},
	"command":    {},
	"builtin":    {},
	"chroot":     {before: 1},
	"unshare":    {},
	"nsenter":    {valued: []string{"-t", "-S", "-G", "--target"}},
	"stdbuf":     {valued: []string{"-i", "-o", "-e", "--input", "--output", "--error"}},
	"unbuffer":   {},
	"caffeinate": {},
	"xvfb-run":   {valued: []string{"-n", "-s", "-f", "-e", "-w", "--server-num", "--server-args", "--auth-file", "--error-file", "--wait"}},
	"eatmydata":  {},
	"rlwrap":     {},
	// Measure or trace it.
	"time":     {valued: []string{"-f", "-o", "--format", "--output"}},
	"strace":   {valued: []string{"-e", "-o", "-p", "-s", "-u", "-E", "-P"}},
	"ltrace":   {valued: []string{"-e", "-o", "-p", "-s", "-u"}},
	"valgrind": {},
	// Run it for each item, or somewhere else.
	"xargs":        {valued: []string{"-n", "-P", "-I", "-L", "-d", "-a", "-E", "-s", "--max-args", "--max-procs", "--replace", "--delimiter", "--arg-file"}},
	"parallel":     {valued: []string{"-j", "-n", "-N", "-S", "-a", "--jobs", "--max-args", "--sshlogin", "--arg-file"}},
	"ssh":          {valued: []string{"-p", "-i", "-l", "-o", "-F", "-J", "-L", "-R", "-D", "-b", "-c", "-E", "-e", "-I", "-m", "-O", "-Q", "-S", "-W", "-w"}, before: 1},
	"proxychains":  {valued: []string{"-f"}},
	"proxychains4": {valued: []string{"-f"}},
	"torsocks":     {},
	// Subcommands that run one.
	"docker exec": {valued: []string{"-e", "-u", "-w", "--env", "--user", "--workdir", "--env-file", "--detach-keys"}, before: 1},
	"podman exec": {valued: []string{"-e", "-u", "-w", "--env", "--user", "--workdir", "--env-file", "--detach-keys"}, before: 1},
	"docker run": {valued: []string{"-e", "-v", "-p", "-w", "-u", "-l", "-h", "-m", "--name", "--network", "--memory", "--cpus", "--label", "--env", "--env-file",
		"--mount", "--hostname", "--restart", "--platform", "--pull", "--user", "--workdir", "--volume", "--publish", "--add-host", "--device", "--dns", "--entrypoint"}, before: 1},
	"podman run": {valued: []string{"-e", "-v", "-p", "-w", "-u", "-l", "-h", "-m", "--name", "--network", "--memory", "--cpus", "--label", "--env", "--env-file",
		"--mount", "--hostname", "--restart", "--platform", "--pull", "--user", "--workdir", "--volume", "--publish", "--add-host", "--device", "--dns", "--entrypoint"}, before: 1},
	"kubectl exec": {valued: []string{"-n", "-c", "-f", "--namespace", "--container", "--filename", "--context", "--kubeconfig"}, before: 1},
	"uv run":       {valued: []string{"--with", "--python", "-p", "--project", "--directory", "--env-file", "--extra"}},
	"poetry run":   {},
	"pdm run":      {},
	"hatch run":    {},
	"rye run":      {},
	"pipx run":     {valued: []string{"--spec", "--python"}},
	"uvx":          {valued: []string{"--from", "--with", "--python", "-p"}},
	"npx":          {valued: []string{"-p", "--package", "-c", "--call"}},
	"pnpm exec":    {},
	"pnpm dlx":     {valued: []string{"-p", "--package"}},
	"yarn exec":    {},
	"npm exec":     {valued: []string{"-p", "--package", "-c", "--call"}},
	"bunx":         {valued: []string{"-p", "--package"}},
	"bundle exec":  {},
	"nix run":      {},
}

// assignment is a VAR=value word before a command.
var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// programName is the program a command runs, as it reads in the status line:
// its name, or for a wrapper such as sudo or timeout the name with the
// command it runs in parentheses, sudo(ls) or timeout(curl), docker exec(ls),
// as many levels as there are. A wrapper with no command after it is its
// name alone.
func programName(fields []string) string {
	for len(fields) > 0 && assignment.MatchString(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	name := path.Base(strings.Trim(fields[0], `'"`))
	shown, rest := name, fields[1:]
	wrap, wraps := wrappers[name]
	if len(fields) > 1 {
		if sub, ok := wrappers[name+" "+fields[1]]; ok {
			wrap, wraps, shown, rest = sub, true, name+" "+fields[1], fields[2:]
		}
	}
	if !wraps {
		return name
	}
	if name == "command" && slices.ContainsFunc(rest, func(word string) bool { return word == "-v" || word == "-V" }) {
		// command -v only looks a program up: it runs nothing.
		return name
	}
	rest = skipFlags(rest, name, wrap)
	for range wrap.before {
		if len(rest) == 0 {
			break
		}
		rest = skipFlags(rest[1:], name, wrap)
	}
	// A command that is itself $(…) or `…` is a part of its own, named apart.
	if len(rest) > 0 && (strings.HasPrefix(rest[0], "$(") || strings.HasPrefix(rest[0], "`")) {
		return name
	}
	if inner := programName(rest); inner != "" {
		return shown + "(" + inner + ")"
	}
	return name
}

// skipFlags leaves out the flags of a wrapper, and the value of those that
// take one, and the -- that ends them, to the command after.
func skipFlags(words []string, name string, wrap wrapper) []string {
	for len(words) > 0 {
		word := words[0]
		switch {
		case word == "--":
			return words[1:]
		case assignment.MatchString(word) && name == "env":
			words = words[1:]
		case strings.HasPrefix(word, "-") && len(word) > 1:
			words = words[1:]
			if slices.Contains(wrap.valued, word) && len(words) > 0 {
				words = words[1:]
			}
		default:
			return words
		}
	}
	return words
}

// programsShown is how many programs of a command the status line names
// before saying how many more.
const programsShown = 3

// programs are the programs a command runs, once each, without arguments:
// the first ones, all of them whatever they are, and how many more.
func programs(command string) string {
	var names []string
	if commands, err := permissions.Commands(command); err == nil {
		for _, each := range commands {
			names = append(names, programName(strings.Fields(each.Text)))
		}
	} else {
		// Still being written: as far as it came.
		names = append(names, programName(strings.Fields(command)))
	}
	var shown []string
	for _, name := range names {
		if name != "" && !slices.Contains(shown, name) {
			shown = append(shown, name)
		}
	}
	switch {
	case len(shown) == 0:
		return "bash"
	case len(shown) > programsShown:
		return strings.Join(shown[:programsShown], ", ") + fmt.Sprintf(" +%d", len(shown)-programsShown)
	}
	return strings.Join(shown, ", ")
}

// mcpTool splits an MCP tool's name, mcp__server__tool.
func mcpTool(name string) (string, string, bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", "", false
	}
	server, tool, ok := strings.Cut(rest, "__")
	return server, tool, ok
}

// stateLabel is the word in its colour, and the detail after it.
func (s state) label(withDetail bool) string {
	text := lipgloss.NewStyle().Foreground(s.tint).Bold(true).Render(s.word)
	if withDetail && s.detail != "" {
		text += lipgloss.NewStyle().Foreground(s.tint).Render(" · " + s.detail)
	}
	return text
}

// alwaysChanging are the built-in tools that change something whatever
// their arguments; others the registry decides, as git and bash by their
// action.
var alwaysChanging = map[string]bool{"write": true, "edit": true, "mkdir": true, "rm": true}

// promptDetail is how far the prompt is read, leaving out the part the
// server had cached: "42% · 3.1k/7.4k", and how much came cached.
func promptDetail(read llm.PromptProgress) string {
	detail := fmt.Sprintf("%d%% · %s/%s", int(read.Share()*100), formatTokens(max(0, read.Done-read.Cached)), formatTokens(read.Total-read.Cached))
	if read.Cached > 0 {
		detail += " · " + formatTokens(read.Cached) + " cached"
	}
	return detail
}
