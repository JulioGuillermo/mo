package tui

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
)

// settingsDraft holds what the settings form edits, as plain strings and
// lists, starting from the defaults plus the user file.
type settingsDraft struct {
	provider      string
	model         string
	mode          string
	load          string
	alwaysLoaded  []string
	confirm       []string
	disabled      []string
	mcpDisabled   []string
	maxOutput     string
	readMaxSize   string
	outputs       string
	calls         string
	outputTurns   string
	window        string
	compactAt     string
	strategy      string
	keepReasoning bool
	systemPrompt  string
	compactPrompt string
	maxTokens     string
	effort        string
	descriptions  map[string]*string

	defaultDescriptions map[string]string
	savedDescriptions   map[string]string

	// modes holds the mode of single tools, MCP servers and skills, by
	// kind and name, "" for the default; saved are those of the file.
	modes      map[modeKind]map[string]*string
	savedModes map[modeKind]map[string]tools.Mode

	search       string
	searchURL    string
	searchKeyEnv string
	searchKey    string
	savedKey     bool
	forgetKey    bool
	images       string
	background   string
	sidebar      string
	statusStyle  string
	bar          string
	// replyRefresh is how often what the model writes is drawn, with
	// its milliseconds or tokens.
	replyRefresh     string
	replyEveryMS     string
	replyEveryTokens string
	icons            string
	keys             string
	memoryDir        string

	instructionsOn    bool
	instructionsFiles string

	skillMode      string
	skillsDisabled []string
	skillDirs      string

	loopEnabled       bool
	agentsEnabled     bool
	agentsDepth       string
	agentsParallel    string
	agentsMax         string
	agentsSteps       string
	agentsModel       string
	loopReplies       string
	loopRepeats       string
	loopStreamRepeats string
	loopWarnings      string

	initialProvider string
	initialModel    string

	permMode string
	allow    string
	deny     string
	project  string
	// projectMode, projectAllow and projectDeny are the rules of the
	// working directory, kept in the user file under project.
	projectMode  string
	projectAllow string
	projectDeny  string
	tryCommand   string

	notifyFinish   bool
	notifyApproval bool
	notifyMethod   string
	notifyFocused  bool
}

func newSettingsDraft(layer config.Config) *settingsDraft {
	cfg := config.UserSettings(layer)
	model := layer.Model
	if model == "" {
		model = cfg.Providers[layer.Provider].Model
	}
	return draftFrom(cfg, layer.Provider, model)
}

// draftFrom is the draft of the settings cfg holds, with the provider and
// model it names.
func draftFrom(cfg config.Config, provider, model string) *settingsDraft {
	draft := &settingsDraft{
		provider:      provider,
		model:         model,
		mode:          string(cfg.Tools.Mode),
		load:          string(cfg.Tools.Load),
		alwaysLoaded:  slices.Clone(cfg.Tools.AlwaysLoaded),
		confirm:       slices.Clone(cfg.Tools.Confirm),
		disabled:      slices.Clone(cfg.Tools.Disabled),
		mcpDisabled:   slices.Clone(cfg.Tools.MCPDisabled),
		maxOutput:     strconv.Itoa(cfg.Tools.MaxOutput),
		readMaxSize:   cfg.Tools.ReadMaxSize,
		outputs:       cfg.Tools.Outputs,
		calls:         cfg.Tools.Calls,
		outputTurns:   strconv.Itoa(cfg.Tools.OutputTurns),
		window:        strconv.Itoa(cfg.Context.Window),
		compactAt:     strconv.FormatFloat(*cfg.Context.CompactAt, 'f', -1, 64),
		strategy:      cfg.Context.Strategy,
		keepReasoning: *cfg.Context.KeepReasoning,
		systemPrompt:  cfg.SystemPrompt,
		descriptions:  map[string]*string{},
		memoryDir:     cfg.Memory.Dir,

		compactPrompt:    cfg.Context.CompactPrompt,
		maxTokens:        strconv.Itoa(cfg.MaxTokens),
		effort:           cfg.Effort,
		search:           cfg.Web.Search,
		searchURL:        cfg.Web.SearchURL,
		searchKeyEnv:     cfg.Web.SearchKeyEnv,
		savedKey:         cfg.Web.SearchKey != "",
		images:           string(cfg.Render.Images),
		background:       cfg.Render.Background,
		sidebar:          cfg.Render.Sidebar,
		statusStyle:      cfg.Render.Status,
		bar:              cfg.Render.Bar,
		replyRefresh:     cfg.Render.ReplyRefresh,
		replyEveryMS:     strconv.Itoa(cfg.Render.ReplyEveryMS),
		replyEveryTokens: strconv.Itoa(cfg.Render.ReplyEveryTokens),
		icons:            cfg.Render.Icons,
		keys:             cfg.Render.Keys,

		instructionsOn:    cfg.Instructions.Enabled == nil || *cfg.Instructions.Enabled,
		instructionsFiles: strings.Join(cfg.Instructions.Files, "\n"),

		skillMode:      string(cfg.Skills.Mode),
		skillsDisabled: slices.Clone(cfg.Skills.Disabled),
		skillDirs:      strings.Join(cfg.Skills.Dirs, "\n"),

		loopEnabled:       *cfg.Loop.Enabled,
		agentsEnabled:     *cfg.Agents.Enabled,
		agentsDepth:       strconv.Itoa(cfg.Agents.Depth),
		agentsParallel:    strconv.Itoa(cfg.Agents.MaxParallel),
		agentsMax:         strconv.Itoa(cfg.Agents.MaxAgents),
		agentsSteps:       strconv.Itoa(cfg.Agents.MaxSteps),
		agentsModel:       cfg.Agents.Model,
		loopReplies:       strconv.Itoa(cfg.Loop.Replies),
		loopRepeats:       strconv.Itoa(cfg.Loop.Repeats),
		loopStreamRepeats: strconv.Itoa(cfg.Loop.StreamRepeats),
		loopWarnings:      strconv.Itoa(*cfg.Loop.Warnings),

		initialProvider: provider,
		initialModel:    model,

		notifyFinish:   *cfg.Notify.Finish,
		notifyApproval: *cfg.Notify.Approval,
		notifyMethod:   cfg.Notify.Method,
		notifyFocused:  *cfg.Notify.Focused,

		permMode: cfg.Permissions.Mode,
		allow:    strings.Join(cfg.Permissions.Allow, "\n"),
		deny:     strings.Join(cfg.Permissions.Deny, "\n"),
	}
	if draft.systemPrompt == "" {
		draft.systemPrompt = agent.DefaultSystemPrompt
	}
	if draft.compactPrompt == "" {
		draft.compactPrompt = agent.DefaultCompactPrompt
	}
	draft.savedDescriptions = cfg.Tools.Descriptions
	draft.modes = map[modeKind]map[string]*string{toolModes: {}, mcpModes: {}, skillModes: {}}
	draft.savedModes = map[modeKind]map[string]tools.Mode{toolModes: cfg.Tools.Modes, mcpModes: cfg.Tools.MCPModes, skillModes: cfg.Skills.Modes}
	draft.describeTools(tools.BuiltinDescriptions())
	return draft
}

// describeTools sets the tools the form shows descriptions for, with their
// own descriptions, and the saved ones in their place.
func (d *settingsDraft) describeTools(defaults map[string]string) {
	d.defaultDescriptions = defaults
	d.descriptions = map[string]*string{}
	for name, description := range defaults {
		if custom, ok := d.savedDescriptions[name]; ok {
			description = custom
		}
		d.descriptions[name] = &description
	}
}

// applyTo writes the draft into the user layer. Provider and model are only
// written when they were changed, and every other value only when it differs
// from the built-in default.
func (d *settingsDraft) applyTo(layer config.Config, findings []detect.Finding) (config.Config, error) {
	maxOutput, err := parseCount(d.maxOutput)
	if err != nil {
		return layer, fmt.Errorf("max tool output: %w", err)
	}
	window, err := parseCount(d.window)
	if err != nil {
		return layer, fmt.Errorf("context window: %w", err)
	}
	compactAt, err := parseFraction(d.compactAt)
	if err != nil {
		return layer, fmt.Errorf("compact at: %w", err)
	}
	maxTokens, err := parseCount(d.maxTokens)
	if err != nil {
		return layer, fmt.Errorf("max output tokens: %w", err)
	}
	agents, err := d.agents()
	if err != nil {
		return layer, err
	}
	layer.Agents = agents
	loop, err := d.loop()
	if err != nil {
		return layer, err
	}

	layer = d.applyProvider(layer, findings)
	defaults := config.Defaults()
	layer.Tools.Mode = unlessDefault(tools.Mode(d.mode), defaults.Tools.Mode)
	layer.Tools.Load = unlessDefault(tools.Load(d.load), defaults.Tools.Load)
	layer.Tools.AlwaysLoaded = unlessDefaultList(d.alwaysLoaded, defaults.Tools.AlwaysLoaded)
	layer.Tools.Confirm = unlessDefaultList(d.confirm, defaults.Tools.Confirm)
	layer.Tools.Disabled = unlessDefaultList(d.disabled, defaults.Tools.Disabled)
	layer.Tools.MCPDisabled = unlessDefaultList(d.mcpDisabled, defaults.Tools.MCPDisabled)
	layer.Tools.Modes = d.modesOf(toolModes)
	layer.Tools.MCPModes = d.modesOf(mcpModes)
	layer.Instructions = config.Instructions{
		Enabled: unlessDefaultFlag(d.instructionsOn, *defaults.Instructions.Enabled),
		Files:   unlessDefaultList(ruleLines(d.instructionsFiles), defaults.Instructions.Files),
	}
	layer.Skills = config.Skills{
		Mode:     unlessDefault(tools.Mode(d.skillMode), defaults.Skills.Mode),
		Disabled: unlessDefaultList(d.skillsDisabled, defaults.Skills.Disabled),
		Dirs:     ruleLines(d.skillDirs),
		Modes:    d.modesOf(skillModes),
	}
	layer.Tools.MaxOutput = unlessDefault(maxOutput, defaults.Tools.MaxOutput)
	if _, err := config.ParseSize(d.readMaxSize); err != nil {
		return layer, fmt.Errorf("max read size: %w", err)
	}
	layer.Tools.ReadMaxSize = unlessDefault(strings.TrimSpace(d.readMaxSize), defaults.Tools.ReadMaxSize)
	outputTurns, err := strconv.Atoi(strings.TrimSpace(orDefault(d.outputTurns, "0")))
	if err != nil || outputTurns < 0 {
		return layer, fmt.Errorf("command output turns: use 0 or a number of turns")
	}
	layer.Tools.Outputs = unlessDefault(d.outputs, defaults.Tools.Outputs)
	layer.Tools.Calls = unlessDefault(d.calls, defaults.Tools.Calls)
	layer.Tools.OutputTurns = unlessDefault(outputTurns, defaults.Tools.OutputTurns)
	layer.Tools.Descriptions = d.changedDescriptions()
	layer.Context.Window = window
	layer.Context.CompactAt = &compactAt
	if compactAt == *defaults.Context.CompactAt {
		layer.Context.CompactAt = nil
	}
	layer.Context.Strategy = unlessDefault(d.strategy, defaults.Context.Strategy)
	layer.Context.KeepReasoning = nil
	if d.keepReasoning != *defaults.Context.KeepReasoning {
		layer.Context.KeepReasoning = &d.keepReasoning
	}
	layer.SystemPrompt = unlessDefault(strings.TrimSpace(d.systemPrompt), agent.DefaultSystemPrompt)
	layer.Memory.Dir = unlessDefault(strings.TrimSpace(d.memoryDir), defaults.Memory.Dir)
	layer.Loop = loop
	layer.MaxTokens = maxTokens
	layer.Effort = d.effort
	layer.Context.CompactPrompt = unlessDefault(strings.TrimSpace(d.compactPrompt), strings.TrimSpace(agent.DefaultCompactPrompt))
	layer.Web.Search = unlessDefault(d.search, defaults.Web.Search)
	layer.Web.SearchURL = strings.TrimSpace(d.searchURL)
	layer.Web.SearchKeyEnv = strings.TrimSpace(d.searchKeyEnv)
	if d.forgetKey {
		layer.Web.SearchKey = ""
	}
	if key := strings.TrimSpace(d.searchKey); key != "" {
		layer.Web.SearchKey = key
	}
	layer.Render.Images = unlessDefault(render.Mode(d.images), defaults.Render.Images)
	if !config.ValidBackground(d.background) {
		return layer, fmt.Errorf("background %q: use #rrggbb or terminal", d.background)
	}
	layer.Render.Background = unlessDefault(strings.TrimSpace(d.background), defaults.Render.Background)
	layer.Render.Sidebar = unlessDefault(d.sidebar, defaults.Render.Sidebar)
	layer.Render.Status = unlessDefault(d.statusStyle, defaults.Render.Status)
	layer.Render.Bar = unlessDefault(d.bar, defaults.Render.Bar)
	everyMS, err := parseAtLeast(d.replyEveryMS, 10)
	if err != nil {
		return layer, fmt.Errorf("reply refresh milliseconds: %w", err)
	}
	everyTokens, err := parseAtLeast(d.replyEveryTokens, 1)
	if err != nil {
		return layer, fmt.Errorf("reply refresh tokens: %w", err)
	}
	layer.Render.ReplyRefresh = unlessDefault(d.replyRefresh, defaults.Render.ReplyRefresh)
	layer.Render.ReplyEveryMS = unlessDefault(everyMS, defaults.Render.ReplyEveryMS)
	layer.Render.ReplyEveryTokens = unlessDefault(everyTokens, defaults.Render.ReplyEveryTokens)
	layer.Render.Icons = unlessDefault(d.icons, defaults.Render.Icons)
	layer.Render.Keys = unlessDefault(d.keys, defaults.Render.Keys)
	layer.Notify = config.Notify{
		Finish:   unlessDefaultFlag(d.notifyFinish, *defaults.Notify.Finish),
		Approval: unlessDefaultFlag(d.notifyApproval, *defaults.Notify.Approval),
		Method:   unlessDefault(d.notifyMethod, defaults.Notify.Method),
		Focused:  unlessDefaultFlag(d.notifyFocused, *defaults.Notify.Focused),
	}
	return d.applyPermissions(layer)
}

// unlessDefaultFlag is nil for a flag left at its default, so the file
// keeps only what was changed.
func unlessDefaultFlag(value, builtin bool) *bool {
	if value == builtin {
		return nil
	}
	return &value
}

// forProject adds the rules the user file keeps for workDir to the draft.
func (d *settingsDraft) forProject(workDir string, layer config.Config) {
	d.project = config.ProjectKey(workDir)
	rules := layer.Permissions.Projects[d.project]
	d.projectMode = rules.Mode
	d.projectAllow = strings.Join(rules.Allow, "\n")
	d.projectDeny = strings.Join(rules.Deny, "\n")
}

// applyPermissions writes the mode and rules, refusing a rule that does not
// compile, and the working directory's rules when there are any.
func (d *settingsDraft) applyPermissions(layer config.Config) (config.Config, error) {
	for _, field := range []struct{ name, text string }{
		{"allowed commands", d.allow}, {"never without asking", d.deny},
		{"allowed in this project", d.projectAllow}, {"never without asking in this project", d.projectDeny},
	} {
		if err := validRules(field.text); err != nil {
			return layer, fmt.Errorf("%s: %w", field.name, err)
		}
	}
	defaults := config.Defaults().Permissions
	allow := ruleLines(d.allow)
	if allow == nil {
		allow = []string{}
	}
	layer.Permissions.Mode = unlessDefault(d.permMode, defaults.Mode)
	layer.Permissions.Allow = unlessDefaultList(allow, defaults.Allow)
	layer.Permissions.Deny = unlessDefaultList(ruleLines(d.deny), defaults.Deny)
	if d.project == "" {
		return layer, nil
	}
	rules := config.ProjectRules{Mode: d.projectMode, Allow: ruleLines(d.projectAllow), Deny: ruleLines(d.projectDeny)}
	if rules.Mode == "" && rules.Allow == nil && rules.Deny == nil {
		layer.Permissions.Projects = withoutEntry(layer.Permissions.Projects, d.project)
	} else {
		layer.Permissions.Projects = withEntry(layer.Permissions.Projects, d.project, rules)
	}
	if len(layer.Permissions.Projects) == 0 {
		layer.Permissions.Projects = nil
	}
	return layer, nil
}

// tryRules says what the draft's rules do with a command typed to try them.
func (d *settingsDraft) tryRules() string {
	if strings.TrimSpace(d.tryCommand) == "" {
		return "type a command to see which rule decides it"
	}
	mode := permissions.Mode(orDefault(d.projectMode, d.permMode))
	policy, err := permissions.New(mode, append(ruleLines(d.allow), ruleLines(d.projectAllow)...), append(ruleLines(d.deny), ruleLines(d.projectDeny)...))
	if err != nil {
		return err.Error()
	}
	commands, err := permissions.Commands(d.tryCommand)
	if err != nil {
		return "does not parse, so it asks: " + err.Error()
	}
	var lines []string
	for _, command := range commands {
		verdict, rule := policy.Check(command.Text)
		line := command.Text + " → "
		switch {
		case verdict == permissions.Denied:
			line += "asks always (deny: " + rule + ")"
		case verdict == permissions.Allowed && command.Writes:
			line += "asks: it writes to a file (allow: " + rule + ")"
		case verdict == permissions.Allowed:
			line += "runs (allow: " + rule + ")"
		case mode == permissions.ModeAll:
			line += "runs: mode all"
		case mode == permissions.ModeAuto:
			line += "the model judges it"
		default:
			line += "asks"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// agents reads the subagents' fields, keeping only what differs from the
// built-in defaults.
func (d *settingsDraft) agents() (config.Agents, error) {
	defaults := config.Defaults().Agents
	var counts [4]int
	for i, field := range []struct{ name, text string }{
		{"subagent levels", d.agentsDepth}, {"subagents at once", d.agentsParallel}, {"subagents per turn", d.agentsMax}, {"subagent steps", d.agentsSteps},
	} {
		value, err := parseAtLeast(field.text, 1)
		if err != nil {
			return config.Agents{}, fmt.Errorf("%s: %w", field.name, err)
		}
		counts[i] = value
	}
	model := strings.TrimSpace(d.agentsModel)
	if model != "" && !strings.Contains(model, "/") {
		return config.Agents{}, fmt.Errorf("subagents' model: use provider/model, or leave it empty")
	}
	agents := config.Agents{
		Depth:       unlessDefault(counts[0], defaults.Depth),
		MaxParallel: unlessDefault(counts[1], defaults.MaxParallel),
		MaxAgents:   unlessDefault(counts[2], defaults.MaxAgents),
		MaxSteps:    unlessDefault(counts[3], defaults.MaxSteps),
		Model:       model,
	}
	if d.agentsEnabled != *defaults.Enabled {
		agents.Enabled = &d.agentsEnabled
	}
	return agents, nil
}

// loop reads the loop guard fields, keeping only what differs from the
// built-in defaults.
func (d *settingsDraft) loop() (config.Loop, error) {
	defaults := config.Defaults().Loop
	replies, err := parseAtLeast(d.loopReplies, 1)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop replies: %w", err)
	}
	repeats, err := parseAtLeast(d.loopRepeats, 2)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop repeats: %w", err)
	}
	streamRepeats, err := parseAtLeast(d.loopStreamRepeats, 2)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop stream repeats: %w", err)
	}
	warnings, err := parseAtLeast(d.loopWarnings, 0)
	if err != nil {
		return config.Loop{}, fmt.Errorf("loop warnings: %w", err)
	}
	loop := config.Loop{
		Replies:       unlessDefault(replies, defaults.Replies),
		Repeats:       unlessDefault(repeats, defaults.Repeats),
		StreamRepeats: unlessDefault(streamRepeats, defaults.StreamRepeats),
	}
	if d.loopEnabled != *defaults.Enabled {
		loop.Enabled = &d.loopEnabled
	}
	if warnings != *defaults.Warnings {
		loop.Warnings = &warnings
	}
	return loop, nil
}

// applyProvider records a changed provider, adding a detected one to the
// providers, and a changed model where the user file already keeps it, or
// as the top-level model when the provider is picked automatically.
func (d *settingsDraft) applyProvider(layer config.Config, findings []detect.Finding) config.Config {
	model := strings.TrimSpace(d.model)
	providerChanged := d.provider != d.initialProvider
	modelChanged := model != d.initialModel
	if !providerChanged && !modelChanged {
		return layer
	}
	layer.Providers = maps.Clone(layer.Providers)
	if layer.Providers == nil {
		layer.Providers = map[string]config.ProviderConfig{}
	}
	if providerChanged {
		layer.Provider = d.provider
		_, known := config.Defaults().Providers[d.provider]
		_, saved := layer.Providers[d.provider]
		if found := findingFor(findings, d.provider); found != nil && found.Settings != nil && !known && !saved {
			layer.Providers[d.provider] = *found.Settings
		}
	}
	if modelChanged {
		if layer.Model != "" || d.provider == autoProvider {
			layer.Model = model
		} else {
			entry := layer.Providers[d.provider]
			entry.Model = model
			layer.Providers[d.provider] = entry
		}
	}
	return layer
}

// changedDescriptions keeps the edited descriptions that differ from the
// tool's own, and the saved ones of tools not loaded now.
func (d *settingsDraft) changedDescriptions() map[string]string {
	changed := map[string]string{}
	for name, description := range d.savedDescriptions {
		if _, shown := d.descriptions[name]; !shown {
			changed[name] = description
		}
	}
	for name, description := range d.descriptions {
		trimmed := strings.TrimSpace(*description)
		if trimmed != strings.TrimSpace(d.defaultDescriptions[name]) {
			changed[name] = trimmed
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return changed
}

func parseCount(text string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("use a whole number, 0 for the default")
	}
	return value, nil
}

func parseAtLeast(text string, least int) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || value < least {
		return 0, fmt.Errorf("use a whole number, %d or more", least)
	}
	return value, nil
}

func parseFraction(text string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || value < 0 || value >= 1 {
		return 0, fmt.Errorf("use a fraction from 0 (never) to below 1")
	}
	return value, nil
}

func unlessDefault[T comparable](value, builtin T) T {
	var zero T
	if value == builtin {
		return zero
	}
	return value
}

// unlessDefaultList returns an empty list, not nil, for "none ticked":
// "confirm = []" asks for nothing, while leaving the key out keeps the default.
func unlessDefaultList(value, builtin []string) []string {
	if slices.Equal(value, builtin) {
		return nil
	}
	if value == nil {
		return []string{}
	}
	return value
}

func findingFor(findings []detect.Finding, provider string) *detect.Finding {
	for i := range findings {
		if findings[i].Provider == provider && findings[i].Usable {
			return &findings[i]
		}
	}
	return nil
}

// followProvider takes the provider and model of layer, chosen in the
// provider form, as the ones the draft starts from.
func (d *settingsDraft) followProvider(layer config.Config) {
	fresh := newSettingsDraft(layer)
	d.provider, d.initialProvider = fresh.provider, fresh.initialProvider
	d.model, d.initialModel = fresh.model, fresh.initialModel
}

// snapshot returns what puts the draft back as it is now.
func (d *settingsDraft) snapshot() func() {
	saved := *d
	saved.alwaysLoaded = slices.Clone(d.alwaysLoaded)
	saved.confirm = slices.Clone(d.confirm)
	saved.disabled = slices.Clone(d.disabled)
	saved.mcpDisabled = slices.Clone(d.mcpDisabled)
	saved.skillsDisabled = slices.Clone(d.skillsDisabled)
	texts := map[string]string{}
	for name, text := range d.descriptions {
		texts[name] = *text
	}
	modes := map[modeKind]map[string]string{}
	for kind, entries := range d.modes {
		modes[kind] = map[string]string{}
		for name, mode := range entries {
			modes[kind][name] = *mode
		}
	}
	return func() {
		descriptions, modePointers := d.descriptions, d.modes
		*d = saved
		d.descriptions, d.modes = descriptions, modePointers
		for name, text := range texts {
			*descriptions[name] = text
		}
		for kind, entries := range modes {
			for name, mode := range entries {
				*modePointers[kind][name] = mode
			}
		}
	}
}

// modeKind says what a mode is for.
type modeKind int

const (
	toolModes modeKind = iota
	mcpModes
	skillModes
)

// modeOf is the mode of one tool, server or skill, as edited: the file's
// until it is changed.
func (d *settingsDraft) modeOf(kind modeKind, name string) *string {
	if mode, ok := d.modes[kind][name]; ok {
		return mode
	}
	mode := string(d.savedModes[kind][name])
	d.modes[kind][name] = &mode
	return &mode
}

// modesOf is the map of modes to save: those set in the form, and the
// saved ones of names the form did not show.
func (d *settingsDraft) modesOf(kind modeKind) map[string]tools.Mode {
	modes := map[string]tools.Mode{}
	for name, mode := range d.savedModes[kind] {
		modes[name] = mode
	}
	for name, mode := range d.modes[kind] {
		if *mode == "" {
			delete(modes, name)
		} else {
			modes[name] = tools.Mode(*mode)
		}
	}
	if len(modes) == 0 {
		return nil
	}
	return modes
}
