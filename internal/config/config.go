// Package config loads mo's settings: built-in defaults, then the user file
// (~/.config/mo/config.toml), then the project file (.mo/settings.toml). A
// project file may come with the repository, so it can only change what is
// harmless, unless the user saved it from mo and it is unchanged since.
package config

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jg/mo/internal/notify"
	"github.com/jg/mo/internal/peers"
	"github.com/jg/mo/internal/permissions"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/web"
)

type Config struct {
	Provider     string                    `toml:"provider,omitempty"`
	Model        string                    `toml:"model,omitempty"`
	SystemPrompt string                    `toml:"system_prompt,omitempty"`
	MaxTokens    int                       `toml:"max_tokens,omitzero"`
	Effort       string                    `toml:"effort,omitempty"`
	Tools        Tools                     `toml:"tools,omitempty"`
	Context      Context                   `toml:"context,omitempty"`
	Memory       Memory                    `toml:"memory,omitempty"`
	Web          Web                       `toml:"web,omitempty"`
	MCP          map[string]MCPServer      `toml:"mcp,omitempty"`
	Skills       Skills                    `toml:"skills,omitempty"`
	Instructions Instructions              `toml:"instructions,omitempty"`
	Render       Render                    `toml:"render,omitempty"`
	Loop         Loop                      `toml:"loop,omitempty"`
	Permissions  Permissions               `toml:"permissions,omitempty"`
	Notify       Notify                    `toml:"notify,omitempty"`
	Agents       Agents                    `toml:"agents,omitempty"`
	Peers        Peers                     `toml:"peers,omitempty"`
	Providers    map[string]ProviderConfig `toml:"providers,omitempty"`
	// Plugins are the settings of each plugin, by its name: what it
	// declares with mo.setting, and enabled = false to keep it off.
	Plugins map[string]map[string]any `toml:"plugins,omitempty"`
}

type Tools struct {
	Mode         tools.Mode `toml:"mode,omitempty"`
	Load         tools.Load `toml:"load,omitempty"`
	AlwaysLoaded []string   `toml:"always_loaded,omitempty"`
	Confirm      []string   `toml:"confirm,omitempty"`
	Disabled     []string   `toml:"disabled,omitempty"`
	// MCPDisabled are MCP servers not started: set up in the user file,
	// turned off everywhere or in one project.
	MCPDisabled []string `toml:"mcp_disabled,omitempty"`
	MaxOutput   int      `toml:"max_output,omitzero"`
	// ReadMaxSize is the largest file read takes whole, as "10MB"; a
	// larger one is read only in parts, by offset, limit or match. "0"
	// for no limit.
	ReadMaxSize string `toml:"read_max_size,omitempty"`
	// Outputs is where the whole output of commands is kept, to read
	// later: disk, memory until mo exits, or none; OutputTurns for how many
	// of the user's turns, 0 for as long as the conversation.
	Outputs string `toml:"outputs,omitempty"`
	// Calls is when the calls of a reply run: streamed, each as soon as it
	// is written, or reply, all once it ends.
	Calls        string            `toml:"calls,omitempty"`
	OutputTurns  int               `toml:"output_turns,omitzero"`
	Descriptions map[string]string `toml:"descriptions,omitempty"`
	// Modes set the mode of single tools over Mode: full, compact, or
	// search, found with tool_search.
	Modes map[string]tools.Mode `toml:"modes,omitempty"`
	// MCPModes set the mode of all the tools of an MCP server, or group:
	// one entry in tool_search for the server, which finds all its tools.
	MCPModes map[string]tools.Mode `toml:"mcp_modes,omitempty"`
}

// Instructions are files that tell the agent how to work, like AGENTS.md:
// the user's in ~/.config/mo, and the project's from the repository's root
// down to the working directory, added to the system prompt.
type Instructions struct {
	Enabled *bool    `toml:"enabled,omitempty"`
	Files   []string `toml:"files,omitempty"`
}

// Skills are folders with a SKILL.md the model may load. Mode shows them in
// full (names and descriptions), compact (names) or search (found with
// skill_list and skill_search). Dirs are more folders of skills besides
// ~/.config/mo/skills and the project's .mo/skills.
type Skills struct {
	Mode     tools.Mode `toml:"mode,omitempty"`
	Disabled []string   `toml:"disabled,omitempty"`
	Dirs     []string   `toml:"dirs,omitempty"`
	// Modes set the mode of single skills over Mode.
	Modes map[string]tools.Mode `toml:"modes,omitempty"`
}

// Reasoning is how much of its own reasoning the model gets back: "off",
// "turn" (that of the turn in progress) or "all". Files from before it was
// a choice have true, which is "all", and false, which is "off".
type Reasoning string

const (
	ReasoningOff  Reasoning = "off"
	ReasoningTurn Reasoning = "turn"
	ReasoningAll  Reasoning = "all"
)

// UnmarshalTOML reads a choice, or the true or false of older files.
func (r *Reasoning) UnmarshalTOML(data any) error {
	switch value := data.(type) {
	case bool:
		*r = ReasoningOff
		if value {
			*r = ReasoningAll
		}
	case string:
		*r = Reasoning(value)
	default:
		return fmt.Errorf("keep_reasoning %v: use off, turn or all", data)
	}
	return nil
}

// Valid tells whether it is one of the three choices.
func (r Reasoning) Valid() bool {
	return r == ReasoningOff || r == ReasoningTurn || r == ReasoningAll
}

type Context struct {
	Window        int       `toml:"window,omitzero"`
	CompactAt     *float64  `toml:"compact_at,omitempty"`
	CompactPrompt string    `toml:"compact_prompt,omitempty"`
	Strategy      string    `toml:"strategy,omitempty"`
	KeepReasoning Reasoning `toml:"keep_reasoning,omitempty"`
}

// Notify says when mo calls the user back and how: at the end of a turn,
// when it asks for permission; by default only while the terminal is not
// focused.
type Notify struct {
	Finish   *bool  `toml:"finish,omitempty"`
	Approval *bool  `toml:"approval,omitempty"`
	Method   string `toml:"method,omitempty"`
	Focused  *bool  `toml:"focused,omitempty"`
}

// Permissions are the mode tool calls start in and the rules for shell
// commands and fetches. Projects holds rules that apply only in one
// working directory; it lives in the user file, never in a project's.
type Permissions struct {
	Mode     string                  `toml:"mode,omitempty"`
	Allow    []string                `toml:"allow,omitempty"`
	Deny     []string                `toml:"deny,omitempty"`
	Projects map[string]ProjectRules `toml:"projects,omitempty"`
}

// Rules are the rules in force: mo's own that only read, then the user's,
// which may take them back with !.
func (p Permissions) Rules() []string {
	return slices.Concat(DefaultAllow, DefaultAllowPowerShell, p.Allow)
}

// ProjectRules add to the user's rules in one project, and may pick another
// mode there.
type ProjectRules struct {
	Mode  string   `toml:"mode,omitempty"`
	Allow []string `toml:"allow,omitempty"`
	Deny  []string `toml:"deny,omitempty"`
}

// Efforts are the efforts mo sends: empty for the model's default, Anthropic's
// low to max, and OpenAI's none and minimal besides. A model that does not
// take the one set answers with an error.
var Efforts = []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"}

// DefaultAllow are the commands that only read and run without asking,
// besides the user's rules; a rule "!ls *" takes one out.
var DefaultAllow = []string{
	"ls *", "pwd", "cat *", "head *", "tail *", "wc *", "grep *", "rg *", "tree *", "file *", "stat *",
	"which *", "echo *", "git status *", "git diff *", "git log *", "git show *", "git branch",
}

// DefaultAllowPowerShell are PowerShell's commands that only read, which
// cover their aliases too: Get-ChildItem is also ls, dir and gci.
var DefaultAllowPowerShell = []string{
	"Get-ChildItem *", "Get-Content *", "Get-Location", "Get-Item *", "Test-Path *", "Resolve-Path *",
	"Select-String *", "Select-Object *", "Sort-Object *", "Where-Object *", "Measure-Object *", "Group-Object *",
	"Format-Table *", "Format-List *", "Out-String *", "Get-Command *", "Write-Output *", "Write-Host *",
}

// Agents lets the model hand tasks to subagents: agents of their own,
// with their own conversation, whose report is all their parent reads
// unless it asks for more.
type Agents struct {
	Enabled *bool `toml:"enabled,omitempty"`
	// Depth is how many levels of subagents there may be: 1, only the
	// main agent starts them; 2, those it started too; and so on.
	Depth int `toml:"depth,omitzero"`
	// MaxParallel is how many work at once; one waiting for its own does
	// not count.
	MaxParallel int `toml:"max_parallel,omitzero"`
	// MaxAgents is how many may start in one of the user's turns.
	MaxAgents int `toml:"max_agents,omitzero"`
	// Model is "provider/model" for the subagents, or empty for the
	// main agent's.
	Model    string `toml:"model,omitempty"`
	MaxSteps int    `toml:"max_steps,omitzero"`
	// Types are kinds of subagent the model picks by name, each with its
	// own model, tools and limits; one without a type is as above.
	Types map[string]AgentType `toml:"types,omitempty"`
	// ListTypes names the types in the agent tool's description; without
	// it the model asks for them.
	ListTypes *bool `toml:"list_types,omitempty"`
}

// AgentType is a kind of subagent. What it leaves empty is as for any
// subagent; it never gets more than the main agent: its tools and skills
// are among the session's, and its permissions only stricter.
type AgentType struct {
	// Description tells the model what the type is for, to pick it.
	Description string `toml:"description,omitempty"`
	// Model is "provider/model"; empty for the subagents' model.
	Model  string `toml:"model,omitempty"`
	Effort string `toml:"effort,omitempty"`
	// Prompt is added to the subagent's system prompt.
	Prompt string `toml:"prompt,omitempty"`
	// CanStart lets it start subagents of its own, while the levels allow.
	CanStart bool `toml:"can_start,omitempty"`
	// Tools and Skills are the only ones it has, by name; * stands for any
	// characters, as mcp__github__*. Empty for all the session has.
	Tools  []string `toml:"tools,omitempty"`
	Skills []string `toml:"skills,omitempty"`
	// Permissions is a mode it runs in when stricter than the session's;
	// Deny adds rules to the session's.
	Permissions string   `toml:"permissions,omitempty"`
	Deny        []string `toml:"deny,omitempty"`
	MaxSteps    int      `toml:"max_steps,omitzero"`
}

// Peers let the main agent see the other mo instances of this user that
// run with peers on, and send them messages with the peers tool.
type Peers struct {
	Enabled *bool `toml:"enabled,omitempty"`
	// Name is what the others call this instance: empty for folder@branch,
	// "auto" for a few words of the conversation's title.
	Name string `toml:"name,omitempty"`
	// Incoming and Outgoing say what asks before a message is let in or
	// sent: one of PeerApprovals.
	Incoming string `toml:"incoming,omitempty"`
	Outgoing string `toml:"outgoing,omitempty"`
	// Wake starts a turn when a message is let in while the model is idle.
	Wake *bool `toml:"wake,omitempty"`
	// Network reaches peers on other computers too: off, visible, found by
	// the others on the local network, or hidden, reached only by address.
	Network string `toml:"network,omitempty"`
	// Port is where this instance takes peers from the network; 0 for any
	// free one, which only visible instances can be found on.
	Port int `toml:"port,omitzero"`
	// Addresses are peers asked directly, as host:port, besides the ones
	// found: hidden ones, or across a VPN.
	Addresses []string `toml:"addresses,omitempty"`
	// Secret is shared by the instances of a team; without it the network
	// stays off. SecretEnv names a variable that holds it instead.
	Secret    string `toml:"secret,omitempty"`
	SecretEnv string `toml:"secret_env,omitempty"`
	// Projects keep another secret for one folder, in the user file only.
	Projects map[string]PeerProject `toml:"projects,omitempty"`
}

// PeerProject is the secret of the peers of one folder.
type PeerProject struct {
	Secret    string `toml:"secret,omitempty"`
	SecretEnv string `toml:"secret_env,omitempty"`
}

// PeerNetworks are what peers reach over the network.
var PeerNetworks = []string{"off", "visible", "hidden"}

// PeerSecretMin is the shortest secret taken.
const PeerSecretMin = peers.SecretMin

// Key is the secret: the one set, else the variable named.
func (p Peers) Key() string {
	if p.Secret != "" {
		return p.Secret
	}
	if p.SecretEnv != "" {
		return os.Getenv(p.SecretEnv)
	}
	return ""
}

// ForProject is the peers' settings in workDir: the folder's secret, when
// the user keeps one, over the global.
func (p Peers) ForProject(workDir string) Peers {
	if own, found := p.Projects[ProjectKey(workDir)]; found && (own.Secret != "" || own.SecretEnv != "") {
		p.Secret, p.SecretEnv = own.Secret, own.SecretEnv
	}
	p.Projects = nil
	return p
}

// PeerApprovals are what may ask before a peer's message: the permission
// mode of the session, the user always, the model judging it, or nothing.
var PeerApprovals = []string{"tools", "ask", "auto", "allow"}

// PeerNameAuto names an instance from its conversation's title.
const PeerNameAuto = "auto"

type Loop struct {
	Enabled       *bool `toml:"enabled,omitempty"`
	Replies       int   `toml:"replies,omitzero"`
	Repeats       int   `toml:"repeats,omitzero"`
	StreamRepeats int   `toml:"stream_repeats,omitzero"`
	Warnings      *int  `toml:"warnings,omitempty"`
	// Steps is how many replies a turn of the main agent may take; 0 for no limit.
	Steps *int `toml:"steps,omitempty"`
}

type Web struct {
	Search       string `toml:"search,omitempty"`
	SearchURL    string `toml:"search_url,omitempty"`
	SearchKey    string `toml:"search_key,omitempty"`
	SearchKeyEnv string `toml:"search_key_env,omitempty"`
}

func (w Web) Key() string {
	if w.SearchKey != "" {
		return w.SearchKey
	}
	return os.Getenv(w.SearchKeyEnv)
}

// MCPServer starts a server with command and args, or reaches one at url.
// Values in env and headers may use $VAR to read the environment.
type MCPServer struct {
	Command string            `toml:"command,omitempty"`
	Args    []string          `toml:"args,omitempty"`
	Env     map[string]string `toml:"env,omitempty"`
	URL     string            `toml:"url,omitempty"`
	Headers map[string]string `toml:"headers,omitempty"`
	Trust   bool              `toml:"trust,omitempty"`
	// CA is a PEM file of certificate authorities trusted for url besides
	// the system's, for a server signed by a private one; ~ is the home
	// folder.
	CA string `toml:"ca,omitempty"`
	// Insecure accepts any certificate from url, self-signed too, so
	// anyone between could read and change the traffic.
	Insecure bool `toml:"insecure,omitempty"`
	// OAuth sets what the sign-in to url cannot find on its own: a client
	// already registered, the scopes, more parameters. Without it, a server
	// that asks for a sign-in is found out and registered with.
	OAuth *OAuth `toml:"oauth,omitempty"`
	// Description says whether what the server says it is for goes to the
	// model when its tools are not in full mode; on unless false.
	Description *bool `toml:"description,omitempty"`
	// DescriptionChars cuts that description; DefaultDescriptionChars when 0.
	DescriptionChars int `toml:"description_chars,omitempty"`
	// Plugin is the plugin that declared the server, which gives its
	// headers; never in a file.
	Plugin string `toml:"-"`
}

const DefaultDescriptionChars = 2048

// DescriptionLimit is how many characters of the server's description go
// to the model; 0 when none does.
func (s MCPServer) DescriptionLimit() int {
	if s.Description != nil && !*s.Description {
		return 0
	}
	if s.DescriptionChars > 0 {
		return s.DescriptionChars
	}
	return DefaultDescriptionChars
}

// BarStyles are the looks of the context bar, the first the default.
var BarStyles = []string{"shapes", "braille", "circles", "half-circles", "squares", "half-squares", "dots", "rings", "dashes", "shade", "blocks", "halves", "line"}

type Render struct {
	Images render.Mode `toml:"images,omitempty"`
	// Background is the colour mo paints behind everything, so it looks
	// the same in any terminal: the theme's own ("theme"), or #rrggbb;
	// "terminal" keeps the terminal's.
	Background string `toml:"background,omitempty"`
	// Sidebar shows the side panel of tasks and background jobs when the
	// terminal is wide enough ("auto"), always ("on") or never ("off").
	Sidebar string `toml:"sidebar,omitempty"`
	// SidebarWidth is the columns of the side panel, from MinSidebarWidth
	// to MaxSidebarWidth.
	SidebarWidth int `toml:"sidebar_width,omitzero"`
	// Status draws the status line as powerline segments, which need a
	// font with their arrows, or "plain".
	Status string `toml:"status,omitempty"`
	// Icons are Nerd Font icons in the status line and the file tree, or
	// "emoji" for the tree to have emoji (the status line then has plain
	// characters), or "text" for plain characters in both.
	Icons string `toml:"icons,omitempty"`
	// Keys are the usual keys, or "vim": modes to write, move and select,
	// and ctrl+h/j/k/l between the panes.
	Keys string `toml:"keys,omitempty"`
	// Bar is how the context bar is drawn, one of BarStyles.
	Bar string `toml:"bar,omitempty"`
	// ReplyRefresh is how often what the model writes is drawn: at every
	// piece ("live"), every ReplyEveryMS milliseconds ("time") or every
	// ReplyEveryTokens pieces ("tokens"). What asks or ends is drawn at
	// once; drawing less leaves a local model's GPU to it.
	ReplyRefresh     string `toml:"reply_refresh,omitempty"`
	ReplyEveryMS     int    `toml:"reply_every_ms,omitzero"`
	ReplyEveryTokens int    `toml:"reply_every_tokens,omitzero"`
	// The milliseconds between the frames of each animation: the status
	// line's spinner, the arc of what runs, the bubble of a call being
	// written, the spark of reasoning and the beat of what asks.
	SpinnerMS   int `toml:"spinner_ms,omitzero"`
	RunningMS   int `toml:"running_ms,omitzero"`
	WritingMS   int `toml:"writing_ms,omitzero"`
	ReasoningMS int `toml:"reasoning_ms,omitzero"`
	AskingMS    int `toml:"asking_ms,omitzero"`
	// UIFPS is the most times a second a plugin's canvas is drawn again by
	// itself, apart from mo's own animations.
	UIFPS int `toml:"ui_fps,omitzero"`
	// CellSize is the pixels of a terminal cell, "8x16", that sizes kitty
	// images; "auto" asks the terminal.
	CellSize string `toml:"cell_size,omitempty"`
	// Theme is the look of the screen, one of UIThemes, dark or light as
	// Mode says: dark, light, or auto as the terminal is.
	Theme string `toml:"theme,omitempty"`
	Mode  string `toml:"mode,omitempty"`
	// Syntax is how code is coloured, one of SyntaxThemes.
	Syntax string `toml:"syntax,omitempty"`
	// Blame shows, in the file viewer, who last changed the line the
	// cursor is on and in which commit; off unless true.
	Blame *bool `toml:"blame,omitempty"`
}

// The side panel's width in columns.
const (
	DefaultSidebarWidth = 36
	MinSidebarWidth     = 20
	MaxSidebarWidth     = 120
)

// UIThemes are the looks of the screen, mo's own first.
var UIThemes = []string{"mo", "tokyo night", "one", "catppuccin", "material", "dracula", "gruvbox", "nord", "solarized", "monokai"}

// Modes of the look, and the background that is the look's own.
const (
	ModeDark        = "dark"
	ModeLight       = "light"
	ModeAuto        = "auto"
	BackgroundTheme = "theme"
)

// SyntaxThemes are the ways code can be coloured, mo's own first.
var SyntaxThemes = []string{"mo", "mo vivid", "tokyo night", "one", "catppuccin", "material", "dracula", "gruvbox", "nord", "solarized", "monokai"}

// MaxUIFPS is the most frames a second a canvas can ask for.
const MaxUIFPS = 120

// MinFrameMS is the shortest time between an animation's frames.
const MinFrameMS = 20

// Reply refreshes.
const (
	RefreshLive   = "live"
	RefreshTime   = "time"
	RefreshTokens = "tokens"
)

type Memory struct {
	Dir string `toml:"dir,omitempty"`
}

type ProviderConfig struct {
	Protocol   string `toml:"type,omitempty"`
	BaseURL    string `toml:"base_url,omitempty"`
	APIKey     string `toml:"api_key,omitempty"`
	APIKeyEnv  string `toml:"api_key_env,omitempty"`
	APIKeyFrom string `toml:"api_key_from,omitempty"`
	Model      string `toml:"model,omitempty"`
	// Headers are sent with every request, for the servers that want their
	// own client's.
	Headers map[string]string `toml:"headers,omitempty"`
	// OAuth signs in to the provider instead of a key: the token it gives
	// is sent as the bearer and renewed when it expires.
	OAuth *OAuth `toml:"oauth,omitempty"`
	// Preamble, for a cloudcode or claude-code provider, starts the system
	// prompt as Antigravity's or Claude Code's own requests do, followed by
	// a line saying the model runs inside mo; false sends mo's prompt
	// alone, which Claude accounts' tokens may be refused for. Unset is
	// true.
	Preamble *bool `toml:"preamble,omitempty"`
	// TokenFrom borrows the sign-in another app already has: "claude-code",
	// "codex" or "antigravity". mo reads its token each time it is needed
	// and keeps none of its own.
	TokenFrom string `toml:"token_from,omitempty"`
	// TokenRenew lets mo renew a borrowed token that expired, and write the
	// renewed one back for the other app. Codex's and Claude Code's refresh
	// tokens change at each renewal, so without writing it back the other
	// app would be left with one that no longer works; off, mo only uses
	// the token while it is valid.
	TokenRenew bool `toml:"token_renew,omitempty"`
	// Plugin is the plugin that declared the provider, whose functions
	// give its headers and fields; never in a file.
	Plugin string `toml:"-"`
}

// The apps whose sign-in a provider can borrow with token_from.
const (
	TokenFromClaudeCode  = "claude-code"
	TokenFromCodex       = "codex"
	TokenFromAntigravity = "antigravity"
)

// OAuth flows a provider may sign in with.
const (
	// OAuthDevice shows a code to enter on a page, for any terminal.
	OAuthDevice = "device"
	// OAuthBrowser opens the browser and takes the code on a local port,
	// with PKCE.
	OAuthBrowser = "browser"
	// OAuthClient takes a token for the client itself, with no user.
	OAuthClient = "client_credentials"
)

// OAuth is how to sign in to a provider with OAuth 2.
type OAuth struct {
	Flow            string            `toml:"flow,omitempty"`
	ClientID        string            `toml:"client_id,omitempty"`
	ClientSecret    string            `toml:"client_secret,omitempty"`
	ClientSecretEnv string            `toml:"client_secret_env,omitempty"`
	AuthURL         string            `toml:"auth_url,omitempty"`
	TokenURL        string            `toml:"token_url,omitempty"`
	DeviceURL       string            `toml:"device_url,omitempty"`
	Scopes          []string          `toml:"scopes,omitempty"`
	Params          map[string]string `toml:"params,omitempty"`
	// RedirectURL is the loopback address the browser flow listens on, when
	// the client only accepts one registered address; empty: any free port.
	RedirectURL string `toml:"redirect_url,omitempty"`
	// JSON says the token endpoint takes its requests and gives its answers
	// as JSON, not as a form.
	JSON bool `toml:"json,omitempty"`
	// ExchangeURL, when set, is asked for the bearer in exchange for the
	// access token, with the provider's headers: GitHub Copilot gives a
	// short-lived token of its own that way.
	ExchangeURL string `toml:"exchange_url,omitempty"`
	// Resource is the address the token is for (RFC 8707), sent with the
	// sign-in and the code, as MCP servers ask.
	Resource string `toml:"resource,omitempty"`
}

// Secret is the client secret, written or from its variable.
func (o OAuth) Secret() string {
	if o.ClientSecret != "" {
		return o.ClientSecret
	}
	return os.Getenv(o.ClientSecretEnv)
}

// Key returns the API key from the first of api_key, api_key_from and
// api_key_env that is set.
func (p ProviderConfig) Key() string {
	switch {
	case p.APIKey != "":
		return p.APIKey
	case p.APIKeyFrom != "":
		return KeyFrom(p.APIKeyFrom)
	case p.APIKeyEnv != "":
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

// NeedsKey tells whether the provider takes a credential: a key, or a
// sign-in.
func (p ProviderConfig) NeedsKey() bool {
	return p.APIKey != "" || p.APIKeyEnv != "" || p.APIKeyFrom != "" || p.OAuth != nil || p.TokenFrom != ""
}

func Defaults() Config {
	return Config{
		Tools: Tools{
			Mode:         tools.ModeFull,
			Load:         tools.LoadContext,
			AlwaysLoaded: []string{"read", "edit", "write", "bash", "ask", "memory"},
			Confirm:      []string{"bash", "rm", "web_fetch"},
			MaxOutput:    10000,
			ReadMaxSize:  "10MB",
			Outputs:      "disk",
			Calls:        "streamed",
		},
		Context:      Context{CompactAt: ptr(0.75), Strategy: "compact", KeepReasoning: ReasoningTurn},
		Memory:       Memory{Dir: defaultMemoryDir()},
		Skills:       Skills{Mode: tools.ModeFull},
		Instructions: Instructions{Enabled: ptr(true), Files: []string{"AGENTS.md", "AGENT.md"}},
		Web:          Web{Search: web.SearchDuckDuckGo},
		Render: Render{Images: render.ModeAuto, Background: BackgroundTheme, Theme: UIThemes[0], Mode: ModeDark, Sidebar: "auto", SidebarWidth: DefaultSidebarWidth, Status: "powerline", Icons: "nerd", Keys: "default", Bar: "shapes",
			ReplyRefresh: RefreshLive, ReplyEveryMS: 100, ReplyEveryTokens: 10,
			SpinnerMS: 80, RunningMS: 150, WritingMS: 220, ReasoningMS: 160, AskingMS: 500, UIFPS: 20, CellSize: render.CellSizeAuto,
			Syntax: SyntaxThemes[0]},
		Loop:        Loop{Enabled: ptr(true), Replies: 30, Repeats: 3, StreamRepeats: 5, Warnings: ptr(2), Steps: ptr(300)},
		Agents:      Agents{Enabled: ptr(false), ListTypes: ptr(false), Depth: 1, MaxParallel: 3, MaxAgents: 20, MaxSteps: 50},
		Peers:       Peers{Enabled: ptr(false), Incoming: "ask", Outgoing: "tools", Wake: ptr(true), Network: "off"},
		Permissions: Permissions{Mode: "edits"},
		Notify:      Notify{Finish: ptr(true), Approval: ptr(true), Method: "auto", Focused: ptr(false)},
		Providers:   presets(),
	}
}

func UserFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mo", "config.toml")
}

func defaultMemoryDir() string {
	return filepath.Join(dataHome(), "mo", "memory")
}

func PluginDir() string {
	return filepath.Join(ConfigHome(), "mo", "plugins")
}

func LogDir() string {
	return filepath.Join(dataHome(), "mo", "logs")
}

func Load(workDir string) (Config, error) {
	user, err := readOptional(UserFile())
	if err != nil {
		return Defaults(), err
	}
	return LoadWithUserLayer(workDir, user)
}

func validate(cfg Config) error {
	modes := []tools.Mode{tools.ModeFull, tools.ModeCompact, tools.ModeSearch}
	if !slices.Contains(modes, cfg.Tools.Mode) {
		return fmt.Errorf("tools.mode %q: use full, compact or search", cfg.Tools.Mode)
	}
	if !slices.Contains(Efforts, cfg.Effort) {
		return fmt.Errorf("effort %q: use %s, or leave it empty for the model's default", cfg.Effort, strings.Join(Efforts[1:], ", "))
	}
	if !slices.Contains(modes, cfg.Skills.Mode) {
		return fmt.Errorf("skills.mode %q: use full, compact or search", cfg.Skills.Mode)
	}
	for _, each := range []struct {
		name  string
		modes map[string]tools.Mode
		group bool
	}{{"tools.modes", cfg.Tools.Modes, false}, {"skills.modes", cfg.Skills.Modes, false}, {"tools.mcp_modes", cfg.Tools.MCPModes, true}} {
		for name, mode := range each.modes {
			if !slices.Contains(modes, mode) && !(each.group && mode == tools.ModeGroup) {
				return fmt.Errorf("%s.%s %q: use full, compact or search%s", each.name, name, mode, map[bool]string{true: ", or group"}[each.group])
			}
		}
	}
	if cfg.Tools.Load != tools.LoadContext && cfg.Tools.Load != tools.LoadDeclare {
		return fmt.Errorf("tools.load %q: use context or declare", cfg.Tools.Load)
	}
	imageModes := []render.Mode{render.ModeAuto, render.ModeKitty, render.ModeSixel, render.ModeBlocks, render.ModeOff}
	if !slices.Contains(imageModes, cfg.Render.Images) {
		return fmt.Errorf("render.images %q: use auto, kitty, sixel, blocks or off", cfg.Render.Images)
	}
	if cfg.Context.Strategy != "compact" && cfg.Context.Strategy != "drop" {
		return fmt.Errorf("context.strategy %q: use compact or drop", cfg.Context.Strategy)
	}
	if cfg.Render.Icons != "nerd" && cfg.Render.Icons != "emoji" && cfg.Render.Icons != "text" {
		return fmt.Errorf("render.icons %q: use nerd, emoji or text", cfg.Render.Icons)
	}
	if !slices.Contains(BarStyles, cfg.Render.Bar) {
		return fmt.Errorf("render.bar %q: use %s", cfg.Render.Bar, strings.Join(BarStyles, ", "))
	}
	if cfg.Render.Status != "powerline" && cfg.Render.Status != "plain" {
		return fmt.Errorf("render.status %q: use powerline or plain", cfg.Render.Status)
	}
	if !slices.Contains([]string{RefreshLive, RefreshTime, RefreshTokens}, cfg.Render.ReplyRefresh) {
		return fmt.Errorf("render.reply_refresh %q: use live, time or tokens", cfg.Render.ReplyRefresh)
	}
	for name, ms := range map[string]int{"spinner_ms": cfg.Render.SpinnerMS, "running_ms": cfg.Render.RunningMS,
		"writing_ms": cfg.Render.WritingMS, "reasoning_ms": cfg.Render.ReasoningMS, "asking_ms": cfg.Render.AskingMS} {
		if ms < MinFrameMS {
			return fmt.Errorf("render.%s %d: use %d or more", name, ms, MinFrameMS)
		}
	}
	if cfg.Render.UIFPS < 1 || cfg.Render.UIFPS > MaxUIFPS {
		return fmt.Errorf("render.ui_fps %d: use 1 to %d", cfg.Render.UIFPS, MaxUIFPS)
	}
	if width := cfg.Render.SidebarWidth; width < MinSidebarWidth || width > MaxSidebarWidth {
		return fmt.Errorf("render.sidebar_width %d: use %d to %d columns", width, MinSidebarWidth, MaxSidebarWidth)
	}
	if !slices.Contains(SyntaxThemes, cfg.Render.Syntax) {
		return fmt.Errorf("render.syntax %q: use %s", cfg.Render.Syntax, strings.Join(SyntaxThemes, ", "))
	}
	if _, _, err := render.ParseCellSize(cfg.Render.CellSize); err != nil {
		return fmt.Errorf("render.cell_size: %w", err)
	}
	if cfg.Render.ReplyEveryMS < 10 || cfg.Render.ReplyEveryTokens < 1 {
		return fmt.Errorf("render.reply_every_ms is 10 or more, render.reply_every_tokens 1 or more")
	}
	if _, err := ParseSize(cfg.Tools.ReadMaxSize); err != nil {
		return fmt.Errorf("tools.read_max_size: %w", err)
	}
	if cfg.Tools.Calls != "streamed" && cfg.Tools.Calls != "reply" {
		return fmt.Errorf("tools.calls %q: use streamed or reply", cfg.Tools.Calls)
	}
	if !slices.Contains([]string{"disk", "memory", "none"}, cfg.Tools.Outputs) {
		return fmt.Errorf("tools.outputs %q: use disk, memory or none", cfg.Tools.Outputs)
	}
	if cfg.Tools.OutputTurns < 0 {
		return fmt.Errorf("tools.output_turns %d: use 0 to keep it all, or a number of turns", cfg.Tools.OutputTurns)
	}
	if cfg.Render.Keys != "default" && cfg.Render.Keys != "vim" {
		return fmt.Errorf("render.keys %q: use default or vim", cfg.Render.Keys)
	}
	if !slices.Contains([]string{"auto", "on", "off"}, cfg.Render.Sidebar) {
		return fmt.Errorf("render.sidebar %q: use auto, on or off", cfg.Render.Sidebar)
	}
	if !ValidBackground(cfg.Render.Background) {
		return fmt.Errorf("render.background %q: use theme, #rrggbb or terminal", cfg.Render.Background)
	}
	if !slices.Contains(UIThemes, cfg.Render.Theme) {
		return fmt.Errorf("render.theme %q: use %s", cfg.Render.Theme, strings.Join(UIThemes, ", "))
	}
	if !slices.Contains([]string{ModeDark, ModeLight, ModeAuto}, cfg.Render.Mode) {
		return fmt.Errorf("render.mode %q: use dark, light or auto", cfg.Render.Mode)
	}
	if err := validatePermissions(cfg.Permissions); err != nil {
		return err
	}
	for name, server := range cfg.MCP {
		if server.DescriptionChars < 0 {
			return fmt.Errorf("mcp.%s.description_chars %d: use a number of characters, or 0 for %d", name, server.DescriptionChars, DefaultDescriptionChars)
		}
	}
	if !slices.Contains(notify.Methods, notify.Method(cfg.Notify.Method)) {
		return fmt.Errorf("notify.method %q: use auto, terminal, desktop, bell or off", cfg.Notify.Method)
	}
	if compactAt := *cfg.Context.CompactAt; compactAt < 0 || compactAt >= 1 {
		return fmt.Errorf("context.compact_at %v: use a fraction from 0 (never) to below 1", compactAt)
	}
	if !cfg.Context.KeepReasoning.Valid() {
		return fmt.Errorf("context.keep_reasoning %q: use off, turn or all", cfg.Context.KeepReasoning)
	}
	if err := validateAgents(cfg.Agents); err != nil {
		return err
	}
	if err := validatePeers(cfg.Peers); err != nil {
		return err
	}
	return validateLoop(cfg.Loop)
}

// ValidBackground accepts a #rrggbb colour, "theme" or "terminal".
func ValidBackground(text string) bool {
	text = strings.TrimSpace(text)
	if text == "terminal" || text == BackgroundTheme {
		return true
	}
	if len(text) != 7 || text[0] != '#' {
		return false
	}
	for _, digit := range strings.ToLower(text[1:]) {
		if !strings.ContainsRune("0123456789abcdef", digit) {
			return false
		}
	}
	return true
}

func validatePermissions(rules Permissions) error {
	if !permissions.Mode(rules.Mode).Valid() {
		return fmt.Errorf("permissions.mode %q: use ask, edits, auto or all", rules.Mode)
	}
	for _, text := range slices.Concat(rules.Allow, rules.Deny) {
		if err := permissions.Compile(text); err != nil {
			return fmt.Errorf("permissions: %w", err)
		}
	}
	return nil
}

func validateAgents(agents Agents) error {
	for _, field := range []struct {
		name  string
		value int
	}{{"depth", agents.Depth}, {"max_parallel", agents.MaxParallel}, {"max_agents", agents.MaxAgents}, {"max_steps", agents.MaxSteps}} {
		if field.value < 1 {
			return fmt.Errorf("agents.%s %d: use 1 or more", field.name, field.value)
		}
	}
	if agents.Model != "" && !strings.Contains(agents.Model, "/") {
		return fmt.Errorf("agents.model %q: use provider/model, or leave it empty for the main agent's", agents.Model)
	}
	for name, kind := range agents.Types {
		if err := ValidateAgentType(name, kind); err != nil {
			return fmt.Errorf("agents.types.%s: %w", name, err)
		}
	}
	return nil
}

var agentTypeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ValidateAgentType checks a type of subagent and its name.
func ValidateAgentType(name string, kind AgentType) error {
	switch {
	case !agentTypeName.MatchString(name):
		return errors.New("name it with letters, digits, - and _")
	case kind.Model != "" && !strings.Contains(kind.Model, "/"):
		return fmt.Errorf("model %q: use provider/model, or leave it empty for the subagents' model", kind.Model)
	case !slices.Contains(Efforts, kind.Effort):
		return fmt.Errorf("effort %q: use %s, or leave it empty", kind.Effort, strings.Join(Efforts[1:], ", "))
	case kind.Permissions != "" && !permissions.Mode(kind.Permissions).Valid():
		return fmt.Errorf("permissions %q: use ask, edits, auto or all, or leave it empty for the session's", kind.Permissions)
	case kind.MaxSteps < 0:
		return fmt.Errorf("max_steps %d: use 1 or more, or leave it out", kind.MaxSteps)
	}
	for _, rule := range kind.Deny {
		if err := permissions.Compile(rule); err != nil {
			return fmt.Errorf("deny: %w", err)
		}
	}
	return nil
}

func validatePeers(peers Peers) error {
	for _, each := range []struct{ name, value string }{{"incoming", peers.Incoming}, {"outgoing", peers.Outgoing}} {
		if !slices.Contains(PeerApprovals, each.value) {
			return fmt.Errorf("peers.%s %q: use %s", each.name, each.value, strings.Join(PeerApprovals, ", "))
		}
	}
	if !slices.Contains(PeerNetworks, peers.Network) {
		return fmt.Errorf("peers.network %q: use %s", peers.Network, strings.Join(PeerNetworks, ", "))
	}
	if peers.Port < 0 || peers.Port > 65535 {
		return fmt.Errorf("peers.port %d: use 0 for any, or 1 to 65535", peers.Port)
	}
	for _, address := range peers.Addresses {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("peers.addresses %q: use host:port", address)
		}
	}
	if secret := peers.Secret; secret != "" && len([]rune(secret)) < PeerSecretMin {
		return fmt.Errorf("peers.secret: use %d characters or more; /peers secret new makes one", PeerSecretMin)
	}
	return nil
}

func validateLoop(loop Loop) error {
	if loop.Replies < 1 {
		return fmt.Errorf("loop.replies %d: use 1 or more", loop.Replies)
	}
	if loop.Repeats < 2 {
		return fmt.Errorf("loop.repeats %d: use 2 or more", loop.Repeats)
	}
	if loop.StreamRepeats < 2 {
		return fmt.Errorf("loop.stream_repeats %d: use 2 or more", loop.StreamRepeats)
	}
	if *loop.Warnings < 0 {
		return fmt.Errorf("loop.warnings %d: use 0 or more", *loop.Warnings)
	}
	if *loop.Steps < 0 {
		return fmt.Errorf("loop.steps %d: use 0 or more", *loop.Steps)
	}
	return nil
}

func readOptional(path string) (Config, error) {
	var layer Config
	if path == "" {
		return layer, nil
	}
	_, err := toml.DecodeFile(path, &layer)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return layer, fmt.Errorf("%s: %w", path, err)
	}
	return layer, nil
}

func merge(base, layer Config) Config {
	base = mergeHarmless(base, layer)
	base.Provider = override(base.Provider, layer.Provider)
	base.Memory.Dir = override(base.Memory.Dir, expandHome(layer.Memory.Dir))
	base.Web.Search = override(base.Web.Search, layer.Web.Search)
	base.Web.SearchURL = override(base.Web.SearchURL, layer.Web.SearchURL)
	base.Web.SearchKey = override(base.Web.SearchKey, layer.Web.SearchKey)
	base.Web.SearchKeyEnv = override(base.Web.SearchKeyEnv, layer.Web.SearchKeyEnv)
	for name, server := range layer.MCP {
		if base.MCP == nil {
			base.MCP = map[string]MCPServer{}
		}
		base.MCP[name] = server
	}
	if layer.Tools.Confirm != nil {
		base.Tools.Confirm = layer.Tools.Confirm
	}
	if layer.Tools.Disabled != nil {
		base.Tools.Disabled = layer.Tools.Disabled
	}
	if layer.Tools.MCPDisabled != nil {
		base.Tools.MCPDisabled = layer.Tools.MCPDisabled
	}
	if layer.Skills.Disabled != nil {
		base.Skills.Disabled = layer.Skills.Disabled
	}
	if layer.Skills.Dirs != nil {
		base.Skills.Dirs = nil
		for _, dir := range layer.Skills.Dirs {
			base.Skills.Dirs = append(base.Skills.Dirs, expandHome(dir))
		}
	}
	for name, provider := range layer.Providers {
		base.Providers[name] = mergeProvider(base.Providers[name], provider)
	}
	if layer.Plugins != nil {
		base.Plugins = layer.Plugins
	}
	base.Permissions = mergePermissions(base.Permissions, layer.Permissions)
	base.Notify = mergeNotify(base.Notify, layer.Notify)
	base.Agents = mergeAgents(base.Agents, layer.Agents)
	base.Peers = mergePeers(base.Peers, layer.Peers)
	return base
}

// mergePeers takes the peers' settings from the user file only: a project
// could otherwise open the agent to messages.
func mergePeers(base, layer Peers) Peers {
	for _, field := range []struct{ base, layer **bool }{{&base.Enabled, &layer.Enabled}, {&base.Wake, &layer.Wake}} {
		if *field.layer != nil {
			*field.base = *field.layer
		}
	}
	base.Name = override(base.Name, layer.Name)
	base.Incoming = override(base.Incoming, layer.Incoming)
	base.Outgoing = override(base.Outgoing, layer.Outgoing)
	base.Network = override(base.Network, layer.Network)
	base.Port = override(base.Port, layer.Port)
	base.Secret = override(base.Secret, layer.Secret)
	base.SecretEnv = override(base.SecretEnv, layer.SecretEnv)
	if layer.Addresses != nil {
		base.Addresses = layer.Addresses
	}
	for dir, own := range layer.Projects {
		if base.Projects == nil {
			base.Projects = map[string]PeerProject{}
		}
		base.Projects[dir] = own
	}
	return base
}

func mergeNotify(base, layer Notify) Notify {
	base.Method = override(base.Method, layer.Method)
	for _, field := range []struct{ base, layer **bool }{
		{&base.Finish, &layer.Finish}, {&base.Approval, &layer.Approval}, {&base.Focused, &layer.Focused},
	} {
		if *field.layer != nil {
			*field.base = *field.layer
		}
	}
	return base
}

func mergePermissions(base, layer Permissions) Permissions {
	base.Mode = override(base.Mode, layer.Mode)
	if layer.Allow != nil {
		base.Allow = layer.Allow
	}
	if layer.Deny != nil {
		base.Deny = layer.Deny
	}
	for dir, rules := range layer.Projects {
		if base.Projects == nil {
			base.Projects = map[string]ProjectRules{}
		}
		base.Projects[dir] = rules
	}
	return base
}

// ForProject is the mode and rules in force in workDir: the user's, with
// those the user set for that directory added.
func (p Permissions) ForProject(workDir string) Permissions {
	rules := p.Projects[ProjectKey(workDir)]
	p.Mode = override(p.Mode, rules.Mode)
	p.Allow = append(slices.Clone(p.Allow), rules.Allow...)
	p.Deny = append(slices.Clone(p.Deny), rules.Deny...)
	p.Projects = nil
	return p
}

// ProjectKey is how a working directory is named in permissions.projects.
func ProjectKey(workDir string) string {
	if absolute, err := filepath.Abs(workDir); err == nil {
		return absolute
	}
	return filepath.Clean(workDir)
}

// mergeProject applies a project file without letting it redirect requests,
// read other keys, spend a paid API, move the memory, skip a confirmation or
// bring back a disabled tool: besides the harmless settings it may pick a
// provider that needs no key, set models, and add tools to confirm or disable.
func mergeProject(base, layer Config) Config {
	base = mergeHarmless(base, layer)
	if chosen, ok := base.Providers[layer.Provider]; ok && !chosen.NeedsKey() {
		base.Provider = layer.Provider
	}
	base.Tools.Confirm = union(base.Tools.Confirm, layer.Tools.Confirm)
	base.Tools.Disabled = union(base.Tools.Disabled, layer.Tools.Disabled)
	base.Tools.MCPDisabled = union(base.Tools.MCPDisabled, layer.Tools.MCPDisabled)
	base.Skills.Disabled = union(base.Skills.Disabled, layer.Skills.Disabled)
	for name, provider := range layer.Providers {
		if known, ok := base.Providers[name]; ok {
			known.Model = override(known.Model, provider.Model)
			base.Providers[name] = known
		}
	}
	base.Permissions.Mode = string(permissions.Stricter(permissions.Mode(base.Permissions.Mode), permissions.Mode(layer.Permissions.Mode)))
	base.Permissions.Deny = union(base.Permissions.Deny, layer.Permissions.Deny)
	return base
}

// mergeHarmless applies the settings any layer may change.
func mergeHarmless(base, layer Config) Config {
	base.Model = override(base.Model, layer.Model)
	base.SystemPrompt = override(base.SystemPrompt, layer.SystemPrompt)
	base.MaxTokens = override(base.MaxTokens, layer.MaxTokens)
	base.Effort = override(base.Effort, layer.Effort)
	base.Tools.Mode = override(base.Tools.Mode, layer.Tools.Mode)
	base.Tools.Modes = mergeModes(base.Tools.Modes, layer.Tools.Modes)
	base.Tools.MCPModes = mergeModes(base.Tools.MCPModes, layer.Tools.MCPModes)
	base.Skills.Modes = mergeModes(base.Skills.Modes, layer.Skills.Modes)
	base.Tools.Load = override(base.Tools.Load, layer.Tools.Load)
	base.Skills.Mode = override(base.Skills.Mode, layer.Skills.Mode)
	if layer.Instructions.Enabled != nil {
		base.Instructions.Enabled = layer.Instructions.Enabled
	}
	if layer.Instructions.Files != nil {
		base.Instructions.Files = layer.Instructions.Files
	}
	base.Tools.MaxOutput = override(base.Tools.MaxOutput, layer.Tools.MaxOutput)
	base.Tools.ReadMaxSize = override(base.Tools.ReadMaxSize, layer.Tools.ReadMaxSize)
	base.Tools.Outputs = override(base.Tools.Outputs, layer.Tools.Outputs)
	base.Tools.Calls = override(base.Tools.Calls, layer.Tools.Calls)
	base.Tools.OutputTurns = override(base.Tools.OutputTurns, layer.Tools.OutputTurns)
	if layer.Tools.AlwaysLoaded != nil {
		base.Tools.AlwaysLoaded = layer.Tools.AlwaysLoaded
	}
	for tool, description := range layer.Tools.Descriptions {
		if base.Tools.Descriptions == nil {
			base.Tools.Descriptions = map[string]string{}
		}
		base.Tools.Descriptions[tool] = description
	}
	base.Context.Window = override(base.Context.Window, layer.Context.Window)
	if layer.Context.CompactAt != nil {
		base.Context.CompactAt = layer.Context.CompactAt
	}
	base.Context.CompactPrompt = override(base.Context.CompactPrompt, layer.Context.CompactPrompt)
	base.Context.Strategy = override(base.Context.Strategy, layer.Context.Strategy)
	base.Context.KeepReasoning = override(base.Context.KeepReasoning, layer.Context.KeepReasoning)
	base.Render.Images = override(base.Render.Images, layer.Render.Images)
	base.Render.Background = override(base.Render.Background, layer.Render.Background)
	base.Render.Sidebar = override(base.Render.Sidebar, layer.Render.Sidebar)
	base.Render.SidebarWidth = override(base.Render.SidebarWidth, layer.Render.SidebarWidth)
	base.Render.Keys = override(base.Render.Keys, layer.Render.Keys)
	base.Render.Status = override(base.Render.Status, layer.Render.Status)
	base.Render.Bar = override(base.Render.Bar, layer.Render.Bar)
	base.Render.ReplyRefresh = override(base.Render.ReplyRefresh, layer.Render.ReplyRefresh)
	base.Render.ReplyEveryMS = override(base.Render.ReplyEveryMS, layer.Render.ReplyEveryMS)
	base.Render.ReplyEveryTokens = override(base.Render.ReplyEveryTokens, layer.Render.ReplyEveryTokens)
	base.Render.SpinnerMS = override(base.Render.SpinnerMS, layer.Render.SpinnerMS)
	base.Render.RunningMS = override(base.Render.RunningMS, layer.Render.RunningMS)
	base.Render.WritingMS = override(base.Render.WritingMS, layer.Render.WritingMS)
	base.Render.ReasoningMS = override(base.Render.ReasoningMS, layer.Render.ReasoningMS)
	base.Render.AskingMS = override(base.Render.AskingMS, layer.Render.AskingMS)
	base.Render.UIFPS = override(base.Render.UIFPS, layer.Render.UIFPS)
	base.Render.CellSize = override(base.Render.CellSize, layer.Render.CellSize)
	base.Render.Syntax = override(base.Render.Syntax, layer.Render.Syntax)
	base.Render.Theme = override(base.Render.Theme, layer.Render.Theme)
	base.Render.Mode = override(base.Render.Mode, layer.Render.Mode)
	if layer.Render.Blame != nil {
		base.Render.Blame = layer.Render.Blame
	}
	base.Render.Icons = override(base.Render.Icons, layer.Render.Icons)
	base.Loop = mergeLoop(base.Loop, layer.Loop)
	return base
}

// mergeModes sets the modes layer names over base's, one by one.
func mergeModes(base, layer map[string]tools.Mode) map[string]tools.Mode {
	if len(layer) == 0 {
		return base
	}
	merged := maps.Clone(base)
	if merged == nil {
		merged = map[string]tools.Mode{}
	}
	maps.Copy(merged, layer)
	return merged
}

// mergeAgents takes the subagents' settings from the user file only: a
// project could otherwise start agents on a paid model.
func mergeAgents(base, layer Agents) Agents {
	if layer.Enabled != nil {
		base.Enabled = layer.Enabled
	}
	base.Depth = override(base.Depth, layer.Depth)
	base.MaxParallel = override(base.MaxParallel, layer.MaxParallel)
	base.MaxAgents = override(base.MaxAgents, layer.MaxAgents)
	base.Model = override(base.Model, layer.Model)
	base.MaxSteps = override(base.MaxSteps, layer.MaxSteps)
	if layer.Types != nil {
		base.Types = layer.Types
	}
	if layer.ListTypes != nil {
		base.ListTypes = layer.ListTypes
	}
	return base
}

func mergeLoop(base, layer Loop) Loop {
	if layer.Enabled != nil {
		base.Enabled = layer.Enabled
	}
	base.Replies = override(base.Replies, layer.Replies)
	base.Repeats = override(base.Repeats, layer.Repeats)
	base.StreamRepeats = override(base.StreamRepeats, layer.StreamRepeats)
	if layer.Warnings != nil {
		base.Warnings = layer.Warnings
	}
	if layer.Steps != nil {
		base.Steps = layer.Steps
	}
	return base
}

func mergeProvider(base, layer ProviderConfig) ProviderConfig {
	base.Protocol = override(base.Protocol, layer.Protocol)
	base.BaseURL = override(base.BaseURL, layer.BaseURL)
	base.APIKey = override(base.APIKey, layer.APIKey)
	base.APIKeyEnv = override(base.APIKeyEnv, layer.APIKeyEnv)
	base.APIKeyFrom = override(base.APIKeyFrom, layer.APIKeyFrom)
	base.Model = override(base.Model, layer.Model)
	if layer.Headers != nil {
		base.Headers = layer.Headers
	}
	if layer.OAuth != nil {
		base.OAuth = layer.OAuth
	}
	if layer.Preamble != nil {
		base.Preamble = layer.Preamble
	}
	base.TokenFrom = override(base.TokenFrom, layer.TokenFrom)
	base.TokenRenew = base.TokenRenew || layer.TokenRenew
	return base
}

func union(base, extra []string) []string {
	for _, item := range extra {
		if !slices.Contains(base, item) {
			base = append(base, item)
		}
	}
	return base
}

func expandHome(path string) string {
	rest, found := strings.CutPrefix(path, "~/")
	if !found {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}

func ptr[T any](value T) *T {
	return &value
}

func override[T comparable](current, next T) T {
	var zero T
	if next == zero {
		return current
	}
	return next
}

// ActiveMCP are the MCP servers to start: those set up and not turned off.
func (c Config) ActiveMCP() map[string]MCPServer {
	active := map[string]MCPServer{}
	for name, server := range c.MCP {
		if !slices.Contains(c.Tools.MCPDisabled, name) {
			active[name] = server
		}
	}
	return active
}

// SkillDirs are the folders skills are found in, the later winning a name
// found twice: the user's, those added in the settings, the project's.
func (c Config) SkillDirs(workDir string) []string {
	dirs := []string{filepath.Join(ConfigHome(), "mo", "skills")}
	dirs = append(dirs, c.Skills.Dirs...)
	return append(dirs, filepath.Join(ProjectDir(workDir), "skills"))
}

// InstructionFiles are the names of the instructions files to read, none
// when they are turned off.
func (c Config) InstructionFiles() []string {
	if c.Instructions.Enabled != nil && !*c.Instructions.Enabled {
		return nil
	}
	return c.Instructions.Files
}

// SessionDir is where conversations are saved to resume them.
func SessionDir() string {
	return filepath.Join(dataHome(), "mo", "sessions")
}

// normalizeToolNames gives the tools the names they have now, where a file
// written before a tool was renamed has the old one: in the lists of tools and
// in the maps keyed by one. A name set under both is the new one's.
func normalizeToolNames(cfg Config) Config {
	rename := func(names []string) []string {
		if names == nil {
			return nil
		}
		renamed := make([]string, 0, len(names))
		for _, name := range names {
			if name = tools.Canonical(name); !slices.Contains(renamed, name) {
				renamed = append(renamed, name)
			}
		}
		return renamed
	}
	cfg.Tools.AlwaysLoaded = rename(cfg.Tools.AlwaysLoaded)
	cfg.Tools.Confirm = rename(cfg.Tools.Confirm)
	cfg.Tools.Disabled = rename(cfg.Tools.Disabled)
	cfg.Agents.Types = maps.Clone(cfg.Agents.Types)
	for name, kind := range cfg.Agents.Types {
		kind.Tools = rename(kind.Tools)
		cfg.Agents.Types[name] = kind
	}
	if cfg.Tools.Modes != nil {
		modes := make(map[string]tools.Mode, len(cfg.Tools.Modes))
		for name, mode := range cfg.Tools.Modes {
			if current := tools.Canonical(name); current == name || cfg.Tools.Modes[current] == "" {
				modes[current] = mode
			}
		}
		cfg.Tools.Modes = modes
	}
	if cfg.Tools.Descriptions != nil {
		descriptions := make(map[string]string, len(cfg.Tools.Descriptions))
		for name, text := range cfg.Tools.Descriptions {
			if current := tools.Canonical(name); current == name {
				descriptions[current] = text
			} else if _, set := cfg.Tools.Descriptions[current]; !set {
				descriptions[current] = text
			}
		}
		cfg.Tools.Descriptions = descriptions
	}
	return cfg
}
