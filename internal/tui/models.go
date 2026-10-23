package tui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

const (
	modelsTimeout = 10 * time.Second
	typeModel     = "\x00type"
	modelsHelp    = "↑↓ choose · / filter · enter take · esc back"
)

// modelChoice is a list of models to pick one from, for whatever asked.
type modelChoice struct {
	provider string
	current  string
	// check is the agent asked what the server has before the models are
	// listed, so the one marked in use is the server's.
	check *agent.Agent
	pick  func(m model, name string) (tea.Model, tea.Cmd)
	back  func(m model) (tea.Model, tea.Cmd)
}

// modelsMsg brings the models a provider lists.
type modelsMsg struct {
	choice modelChoice
	models []string
	err    error
	// followed is the model and window the server turned out to have.
	followed *modelMsg
}

// listModels asks a provider for its models: an OpenAI-compatible server's
// /models, or Anthropic's Models API.
// A provider a plugin declared is asked through the plugin's client.
func listModels(ctx context.Context, provider string, settings config.ProviderConfig, client *http.Client) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	token := auth.TokenFor(provider, settings)
	switch settings.Protocol {
	case "cloudcode":
		return (&llm.CloudCode{BaseURL: settings.BaseURL, Token: token, Client: client}).ListModels(ctx)
	case "responses":
		return (&llm.Responses{BaseURL: settings.BaseURL, Token: token, Client: client}).ListModels(ctx)
	case "anthropic", "claude-code":
		return (&llm.Anthropic{BaseURL: settings.BaseURL, APIKey: settings.Key(), Token: token, Subscription: settings.Protocol == "claude-code", Client: client}).ListModels(ctx)
	}
	return (&llm.OpenAI{BaseURL: settings.BaseURL, APIKey: settings.Key(), Token: token, Headers: settings.Headers, Client: client}).ListModels(ctx)
}

// chooseModel loads the models of a provider off the UI goroutine, then
// lists them to pick one.
func (m model) chooseModel(settings config.ProviderConfig, choice modelChoice) (tea.Model, tea.Cmd) {
	m.settingsInFlight = true
	note := printText(m.printer.Note("asking " + orDefault(choice.provider, "the provider") + " for its models…"))
	return m, tea.Batch(note, loadModels(settings, choice, m.session.Plugins.ProviderClient(choice.provider, settings)))
}

// loadModels asks the server what it has now, then for its models: the list
// is never kept from an earlier time.
func loadModels(settings config.ProviderConfig, choice modelChoice, client *http.Client) tea.Cmd {
	return func() tea.Msg {
		var followed *modelMsg
		if choice.check != nil {
			choice.check.CheckModel(context.Background(), agent.Hooks{
				OnModel: func(model string, window int) { followed = &modelMsg{model: model, window: window} },
			}, agent.CheckModels)
		}
		models, err := listModels(context.Background(), choice.provider, settings, client)
		return modelsMsg{choice: choice, models: models, err: err, followed: followed}
	}
}

// showModels lists the models, the one in use first marked, to filter and
// pick, or to type a name the list does not have.
func (m model) showModels(msg modelsMsg) (tea.Model, tea.Cmd) {
	if msg.followed == nil {
		return m.showModelList(msg)
	}
	msg.choice.current = msg.followed.model
	followed, note := m.modelChanged(*msg.followed)
	listed, show := followed.(model).showModelList(msg)
	return listed, tea.Batch(note, show)
}

// showModelList is the list of the models, once the server's own are known.
func (m model) showModelList(msg modelsMsg) (tea.Model, tea.Cmd) {
	m.settingsInFlight = false
	choice := msg.choice
	if msg.err != nil || len(msg.models) == 0 {
		why := "it lists none"
		if msg.err != nil {
			why = msg.err.Error()
		}
		return m.typeModelName(choice, "could not list the models: "+why)
	}
	picked := choice.current
	var options []huh.Option[string]
	for _, name := range msg.models {
		label := name
		if name == choice.current {
			label += "  ● in use"
		}
		options = append(options, huh.NewOption(label, name))
	}
	options = append(options, huh.NewOption("✎ type another name", typeModel))
	list := huh.NewSelect[string]().Title("Model · " + choice.provider).
		Description(fmt.Sprintf("%d models · / filters", len(msg.models))).
		Filtering(true).Options(options...).Value(&picked)
	if m.height > 0 {
		list.Height(min(len(options)+3, max(m.height-4, settingsMinHeight)))
	}
	next := func(m model) (tea.Model, tea.Cmd) {
		if picked == typeModel {
			return m.typeModelName(choice, "")
		}
		return choice.pick(m, picked)
	}
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, next: next, back: choice.back, help: modelsHelp})
}

// typeModelName asks for a model by name, when the list does not have it
// or the provider gives none.
func (m model) typeModelName(choice modelChoice, problem string) (tea.Model, tea.Cmd) {
	name := choice.current
	description := "the model's id as the provider names it"
	if problem != "" {
		description = errorStyle().Render(problem) + "\n" + description
	}
	form := newForm(huh.NewGroup(huh.NewInput().Title("Model · " + choice.provider).Description(description).Value(&name))).WithShowHelp(false)
	next := func(m model) (tea.Model, tea.Cmd) { return choice.pick(m, strings.TrimSpace(name)) }
	return m.showSettings(&settingsScreen{form: form, next: next, back: choice.back, help: "enter take · esc back"})
}

// switchModel is /model: a list of the models of the provider in use; the
// one picked is saved where the model is set, the project's file when it
// sets one, else the user's.
func (m model) switchModel() (tea.Model, tea.Cmd) {
	settings := m.session.Config.Providers[m.session.Provider]
	closed := func(m model) (tea.Model, tea.Cmd) { return m, printText(m.printer.Note("model unchanged")) }
	return m.chooseModel(settings, modelChoice{provider: m.session.Provider, current: m.session.Model, check: m.session.Agent, back: closed,
		pick: func(m model, name string) (tea.Model, tea.Cmd) { return m.useModel(name) }})
}

func (m model) useModel(name string) (tea.Model, tea.Cmd) {
	if name == "" || name == m.session.Model {
		return m, printText(m.printer.Note("model unchanged"))
	}
	layer, err := config.ReadUserLayer()
	if err != nil {
		return m.emitError(err)
	}
	if project, err := config.ReadProject(m.session.WorkDir); err == nil && config.ProjectSets(project.Layer)["model"] {
		chosen := project.Layer
		chosen.Model, chosen.Providers = name, nil
		keep := func(layer config.Config) (config.Config, error) { return layer, nil }
		return m.saveOrWait(&settingsScreen{layer: layer, apply: keep, project: &chosen, keepUser: true})
	}
	provider := m.session.Provider
	apply := func(layer config.Config) (config.Config, error) {
		draft := newSettingsDraft(layer)
		draft.provider, draft.initialProvider = orDefault(layer.Provider, provider), layer.Provider
		draft.model = name
		return draft.applyProvider(layer, nil), nil
	}
	return m.saveOrWait(&settingsScreen{layer: layer, apply: apply})
}
