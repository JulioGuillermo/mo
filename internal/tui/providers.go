package tui

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
)

// providerDraft holds what the provider form edits for one provider.
type providerDraft struct {
	name      string
	isNew     bool
	hasPreset bool
	savedKey  bool
	protocol  string
	baseURL   string
	model     string
	keyEnv    string
	key       string
	keyFrom   string
	forgetKey bool
	remove    bool
	inUse     bool
	use       bool

	// oauth is the sign-in being edited; flow "" signs in with a key.
	oauth       config.OAuth
	oauthScopes string
}

func newProviderDraft(name string, cfg config.Config, findings []detect.Finding) *providerDraft {
	if name == newProviderChoice {
		return &providerDraft{isNew: true, protocol: "openai", baseURL: defaultNewURL, use: true}
	}
	settings, known := cfg.Providers[name]
	if found := findingFor(findings, name); !known && found != nil && found.Settings != nil {
		settings = *found.Settings
	}
	_, hasPreset := config.Defaults().Providers[name]
	inUse := cfg.Provider == name
	if inUse && cfg.Model != "" {
		settings.Model = cfg.Model
	}
	draft := &providerDraft{
		name:      name,
		hasPreset: hasPreset,
		savedKey:  settings.APIKey != "",
		protocol:  settings.Protocol,
		baseURL:   settings.BaseURL,
		model:     settings.Model,
		keyEnv:    settings.APIKeyEnv,
		keyFrom:   settings.APIKeyFrom,
		inUse:     inUse,
	}
	if settings.OAuth != nil {
		draft.oauth = *settings.OAuth
		draft.oauthScopes = strings.Join(settings.OAuth.Scopes, " ")
	}
	return draft
}

// settings are the provider as the form has it now, with the key typed or
// the one saved, to ask it for its models.
func (d *providerDraft) settings(cfg config.Config) config.ProviderConfig {
	settings := config.ProviderConfig{Protocol: d.protocol, BaseURL: d.baseURL, APIKeyEnv: d.keyEnv, APIKeyFrom: d.keyFrom}
	if d.oauth.Flow != "" {
		oauth := d.oauth
		settings.OAuth = &oauth
	}
	switch {
	case strings.TrimSpace(d.key) != "":
		settings.APIKey = strings.TrimSpace(d.key)
	case !d.forgetKey:
		settings.APIKey = cfg.Providers[d.name].APIKey
	}
	return settings
}

// entries are the fields of a provider, each with its value: a new one
// also gets a name.
func (d *providerDraft) entries(cfg config.Config) []entry {
	var entries []entry
	if d.isNew {
		entries = append(entries, entry{"Name", func() string { return valueOr(d.name, "required") }, func() huh.Field {
			return huh.NewInput().Title("Name").Description("how mo calls it, e.g. my-server").Validate(validNewName(cfg.Providers)).Value(&d.name)
		}})
	}
	entries = append(entries,
		entry{"Type", func() string {
			return map[string]string{"openai": "OpenAI-compatible", "anthropic": "Anthropic", "cloudcode": "Google Cloud Code (Antigravity)"}[d.protocol]
		}, func() huh.Field {
			return huh.NewSelect[string]().Title("Type").Options(
				huh.NewOption("OpenAI-compatible: llama-server, ollama, vLLM, OpenAI, OpenRouter…", "openai"),
				huh.NewOption("Anthropic", "anthropic"),
				huh.NewOption("Google Cloud Code: Antigravity's Gemini and Claude models", "cloudcode"),
			).Value(&d.protocol)
		}},
		entry{"Base URL", func() string { return valueOr(d.baseURL, "required") }, func() huh.Field {
			return huh.NewInput().Title("Base URL").Description("e.g. http://localhost:8080/v1").Validate(required).Value(&d.baseURL)
		}},
		entry{"Model", func() string { return valueOr(d.model, "the first the server lists") }, func() huh.Field {
			return huh.NewInput().Title("Model").Description("empty: the first the server lists (Anthropic needs one)").Value(&d.model)
		}},
		entry{"API key variable", func() string { return valueOr(d.keyEnv, "none") }, func() huh.Field {
			return huh.NewInput().Title("API key variable").Description("environment variable holding the key; empty if it needs none").Value(&d.keyEnv)
		}},
		entry{"API key", d.keyState, func() huh.Field {
			return huh.NewInput().Title("API key").Description("saved in " + config.UserFile() + "; empty keeps the saved one").
				EchoMode(huh.EchoModePassword).Value(&d.key)
		}},
	)
	if d.savedKey {
		entries = append(entries, entry{"Forget the saved key", func() string { return yesNo(d.forgetKey) }, func() huh.Field {
			return huh.NewConfirm().Title("Forget the saved API key").Value(&d.forgetKey)
		}})
	}
	entries = append(entries, entry{"Sign in", func() string { return valueOr(d.oauth.Flow, "with a key") }, func() huh.Field {
		return huh.NewSelect[string]().Title("Sign in").Description("OAuth 2 instead of a key; /login <provider> signs in").Options(
			huh.NewOption("with a key", ""),
			huh.NewOption("device: a code to enter on a page, from any terminal", config.OAuthDevice),
			huh.NewOption("browser: opens it and takes the answer on a local port, with PKCE", config.OAuthBrowser),
			huh.NewOption("client credentials: the client's own token, no user", config.OAuthClient),
		).Value(&d.oauth.Flow)
	}})
	if d.oauth.Flow != "" {
		text := func(title, description string, value *string) entry {
			return entry{title, func() string { return valueOr(*value, "none") }, func() huh.Field {
				return huh.NewInput().Title(title).Description(description).Value(value)
			}}
		}
		entries = append(entries,
			text("Client id", "the id the provider gave mo's client", &d.oauth.ClientID),
			text("Client secret variable", "environment variable holding the secret, if the client has one", &d.oauth.ClientSecretEnv),
			text("Token URL", "where tokens are taken and renewed", &d.oauth.TokenURL),
		)
		switch d.oauth.Flow {
		case config.OAuthDevice:
			entries = append(entries, text("Device URL", "where the device code is asked for", &d.oauth.DeviceURL))
		case config.OAuthBrowser:
			entries = append(entries,
				text("Authorization URL", "the page that asks the user", &d.oauth.AuthURL),
				text("Redirect URL", "http://localhost:<port>/<path> when the client takes only that one; empty: any free port", &d.oauth.RedirectURL),
			)
		}
		entries = append(entries, text("Scopes", "separated by spaces", &d.oauthScopes))
	}
	if sources := keySources(d.keyFrom); len(sources) > 1 {
		entries = append(entries, entry{"Take the key from", func() string { return valueOr(d.keyFrom, "nowhere else") }, func() huh.Field {
			return huh.NewSelect[string]().Title("Or take the key from").Options(sources...).Value(&d.keyFrom)
		}})
	}
	if !d.inUse {
		entries = append(entries, entry{"Use it from now on", func() string { return yesNo(d.use) }, func() huh.Field {
			return huh.NewConfirm().Title("Use it from now on").Value(&d.use)
		}})
	}
	return entries
}

// fields are the entries as one form, for the setup screen that runs
// before mo starts.
func (d *providerDraft) fields(cfg config.Config) []huh.Field {
	var fields []huh.Field
	for _, each := range d.entries(cfg) {
		fields = append(fields, each.field())
	}
	return fields
}

// keyState says whether a key is typed, saved, or neither.
func (d *providerDraft) keyState() string {
	switch {
	case strings.TrimSpace(d.key) != "":
		return "new one typed"
	case d.savedKey && !d.forgetKey:
		return "saved"
	}
	return "none"
}

// keySources lists the logins of other tools mo can take a key from, and
// the current source even when it is no longer found.
func keySources(current string) []huh.Option[string] {
	options := []huh.Option[string]{huh.NewOption("nowhere else", "")}
	sources := map[string]bool{}
	if config.ReadCodexAuth().UsableKey() != "" {
		sources[config.KeyFromCodex] = true
	}
	for name, entry := range config.OpencodeAuth() {
		if entry.Key != "" {
			sources[config.OpencodeKeyFrom(name)] = true
		}
	}
	if current != "" {
		sources[current] = true
	}
	for _, source := range slices.Sorted(maps.Keys(sources)) {
		options = append(options, huh.NewOption(source+" login", source))
	}
	return options
}

// applyTo writes the provider into the user layer, keeping only what differs
// from the built-in preset. A removed provider that was in use leaves the
// choice to auto. The provider in use takes the top-level model as its own.
func (d *providerDraft) applyTo(layer config.Config) (config.Config, error) {
	name := strings.TrimSpace(d.name)
	if name == "" {
		return layer, errors.New("the provider needs a name")
	}
	if d.remove {
		layer.Providers = withoutEntry(layer.Providers, name)
		if layer.Provider == name && !d.hasPreset {
			layer.Provider = autoProvider
		}
		return layer, nil
	}
	if (d.protocol == "anthropic" || d.protocol == "cloudcode") && strings.TrimSpace(d.model) == "" {
		return layer, fmt.Errorf("%s: this type of provider needs a model", name)
	}
	preset := config.Defaults().Providers[name]
	entry := config.ProviderConfig{
		Protocol:   unlessDefault(d.protocol, preset.Protocol),
		BaseURL:    unlessDefault(strings.TrimSpace(d.baseURL), preset.BaseURL),
		Model:      unlessDefault(strings.TrimSpace(d.model), preset.Model),
		APIKeyEnv:  unlessDefault(strings.TrimSpace(d.keyEnv), preset.APIKeyEnv),
		APIKeyFrom: unlessDefault(d.keyFrom, preset.APIKeyFrom),
		APIKey:     layer.Providers[name].APIKey,
	}
	if d.forgetKey {
		entry.APIKey = ""
	}
	if d.oauth.Flow != "" {
		oauth := d.oauth
		oauth.Scopes = strings.Fields(d.oauthScopes)
		if oauth.ClientID == "" || oauth.TokenURL == "" {
			return layer, fmt.Errorf("%s: signing in needs a client id and a token URL", name)
		}
		if preset.OAuth == nil || !reflect.DeepEqual(oauth, *preset.OAuth) {
			entry.OAuth = &oauth
		}
	}
	if key := strings.TrimSpace(d.key); key != "" {
		entry.APIKey = key
	}
	if d.use || d.inUse {
		layer.Provider = name
		layer.Model = ""
	}
	if entry == (config.ProviderConfig{}) {
		layer.Providers = withoutEntry(layer.Providers, name)
		return layer, nil
	}
	layer.Providers = withEntry(layer.Providers, name, entry)
	return layer, nil
}

// signInActions are the buttons to sign in to the provider, and out of it,
// for one that signs in with the browser or a device code. They work from
// the form as it is, saved or not.
func (d *providerDraft) signInActions(cfg config.Config) []editorAction {
	if d.oauth.Flow == "" || d.oauth.Flow == config.OAuthClient || strings.TrimSpace(d.name) == "" {
		return nil
	}
	name := strings.TrimSpace(d.name)
	label := "Log in"
	var actions []editorAction
	if _, err := auth.Load(name); err == nil {
		label = "Log in again"
		actions = append(actions, editorAction{label: "Log out", run: func(m model) (tea.Model, tea.Cmd) { return m.logout(name) }})
	}
	login := editorAction{label: label, run: func(m model) (tea.Model, tea.Cmd) {
		oauth := d.oauth
		oauth.Scopes = strings.Fields(d.oauthScopes)
		return m.loginTo(name, oauth)
	}}
	return append([]editorAction{login}, actions...)
}

// pickProvider lists the providers to edit, then opens the chosen one. From
// the settings menu, esc goes back to it and an edit waits there to be saved
// with the rest; on its own, an edit is saved at once.
func (m model) pickProvider(msg settingsLoadedMsg, menu *settingsMenu) (tea.Model, tea.Cmd) {
	cfg := config.UserSettings(msg.layer)
	picked := m.session.Provider
	options := providerOptions(cfg, msg.findings, msg.layer.Provider)[1:]
	options = append([]huh.Option[string]{huh.NewOption(keyStyle.Render("Active provider")+"   "+dimStyle.Render(orDefault(msg.layer.Provider, "auto")+" · pick the one mo uses"), activeProviderChoice)}, options...)
	options = append(options, huh.NewOption("+ set up a new provider", newProviderChoice))
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Provider to edit").Options(options...).Value(&picked),
	)).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) {
		if picked == activeProviderChoice {
			return m.chooseActiveProvider(msg, cfg, menu)
		}
		return m.editProvider(msg, cfg, picked, menu)
	}
	screen := &settingsScreen{form: form, layer: msg.layer, next: next, help: menuHelp}
	if menu != nil {
		screen.back = func(m model) (tea.Model, tea.Cmd) { return m.showMenu(menu) }
	}
	return m.showSettings(screen)
}

// chooseActiveProvider picks the provider mo uses, with no field to edit: a
// provider found in another tool and not in the config yet comes along.
func (m model) chooseActiveProvider(msg settingsLoadedMsg, cfg config.Config, menu *settingsMenu) (tea.Model, tea.Cmd) {
	chosen := msg.layer.Provider
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Active provider").Description("the model comes from its own settings; /model picks another").
			Options(providerOptions(cfg, msg.findings, msg.layer.Provider)...).Value(&chosen),
	)).WithShowHelp(false)
	back := func(m model) (tea.Model, tea.Cmd) { return m.pickProvider(msg, menu) }
	apply := func(layer config.Config) (config.Config, error) {
		layer.Provider = chosen
		layer.Model = ""
		if found := findingFor(msg.findings, chosen); found != nil && found.Settings != nil {
			if _, known := config.Defaults().Providers[chosen]; !known {
				if _, saved := layer.Providers[chosen]; !saved {
					layer.Providers = withEntry(layer.Providers, chosen, *found.Settings)
				}
			}
		}
		return layer, nil
	}
	next := func(m model) (tea.Model, tea.Cmd) {
		if menu != nil {
			return m.keepEdit(menu, "provider "+orDefault(chosen, "auto"), apply)
		}
		return m.saveOrWait(&settingsScreen{layer: msg.layer, apply: apply})
	}
	return m.showSettings(&settingsScreen{form: form, next: next, back: back, help: menuHelp})
}

// editProvider fills in a provider a field at a time, then saves it.
func (m model) editProvider(msg settingsLoadedMsg, cfg config.Config, name string, menu *settingsMenu) (tea.Model, tea.Cmd) {
	draft := newProviderDraft(name, cfg, msg.findings)
	title := "Provider " + name
	if draft.isNew {
		title = "New provider"
	}
	e := &editor{
		title:     title,
		entries:   func() []entry { return draft.entries(cfg) },
		saveLabel: "Save",
		back:      func(m model) (tea.Model, tea.Cmd) { return m.pickProvider(msg, menu) },
		snapshot: func() func() {
			saved := *draft
			return func() { *draft = saved }
		},
		models: func() (string, config.ProviderConfig, string, func(string)) {
			return draft.name, draft.settings(cfg), draft.model, func(name string) { draft.model = name }
		},
	}
	what := "provider " + orDefault(name, "new")
	e.actions = draft.signInActions(cfg)
	e.save = func(m model) (tea.Model, tea.Cmd) {
		if draft.isNew {
			if err := validNewName(cfg.Providers)(draft.name); err != nil {
				e.notice = errorStyle.Render("not saved: " + err.Error())
				return m.showEditor(e, "Name")
			}
		}
		return m.saveEdit(e, msg, menu, what, draft.applyTo)
	}
	if !draft.isNew {
		e.removeLabel = "Delete this provider"
		if draft.hasPreset {
			e.removeLabel = "Reset it to the built-in settings"
		}
		e.remove = func(m model) (tea.Model, tea.Cmd) {
			gone := *draft
			gone.remove = true
			return m.saveEdit(e, msg, menu, what, gone.applyTo)
		}
	}
	return m.showEditor(e, "")
}
