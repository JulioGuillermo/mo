package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
)

// The settings of the subagents' provider and model, each a list.
const (
	subagentsProvider = "Subagents' provider"
	subagentsModel    = "Subagents' model"
)

// providerModel is a "provider/model" setting taken apart, as the
// subagents' model and a type's are set: a provider from the list, then
// a model of that provider's.
type providerModel struct {
	provider string
	model    string
}

func splitProviderModel(text string) providerModel {
	provider, model, _ := strings.Cut(strings.TrimSpace(text), "/")
	return providerModel{provider: provider, model: model}
}

// joined is the setting as the config keeps it; empty when no provider is
// picked, and an error when a provider is picked without a model.
func (p providerModel) joined(what string) (string, error) {
	switch {
	case p.provider == "":
		return "", nil
	case strings.TrimSpace(p.model) == "":
		return "", fmt.Errorf("%s: pick a model of %s", what, p.provider)
	}
	return p.provider + "/" + strings.TrimSpace(p.model), nil
}

// providerChoices are the providers the user set up and the ones found
// that can be used, with the one in use and the one picked; the first
// option means none of them: what the setting falls back to.
func (m model) providerChoices(menu *settingsMenu, cfg config.Config, current, fallback string) []huh.Option[string] {
	names := slices.Collect(maps.Keys(menu.layer.Providers))
	for _, found := range menu.loaded.findings {
		if found.Usable {
			names = append(names, found.Provider)
		}
	}
	names = append(names, m.session.Provider, current)
	slices.Sort(names)
	labels := map[string]string{}
	for _, option := range providerOptions(cfg, menu.loaded.findings, current) {
		labels[option.Value] = option.Key
	}
	options := []huh.Option[string]{huh.NewOption(fallback, "")}
	for _, name := range slices.Compact(names) {
		if name != "" {
			options = append(options, huh.NewOption(orDefault(labels[name], name), name))
		}
	}
	return options
}

// pickProviderThenModel shows the providers; picking another one goes on to
// its models, as a provider without a model is no setting.
func (m model) pickProviderThenModel(menu *settingsMenu, title, fallback string, value *providerModel, back func(m model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	cfg := menu.config(m.session.Config)
	picked := value.provider
	list := huh.NewSelect[string]().Title(title).Options(m.providerChoices(menu, cfg, value.provider, fallback)...).Value(&picked)
	next := func(m model) (tea.Model, tea.Cmd) {
		if picked == value.provider {
			return back(m)
		}
		value.provider, value.model = picked, ""
		if picked == "" {
			return back(m)
		}
		return m.pickModelOf(menu, value, back)
	}
	form := newForm(huh.NewGroup(list)).WithShowHelp(false)
	return m.showSettings(&settingsScreen{form: form, layer: menu.layer, next: next, back: back, help: menuHelp})
}

// pickModelOf lists the models of the provider picked, or of the main
// agent's when none is, which it then picks too.
func (m model) pickModelOf(menu *settingsMenu, value *providerModel, back func(m model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	provider := orDefault(value.provider, m.session.Provider)
	settings := menu.config(m.session.Config).Providers[provider]
	return m.chooseModel(settings, modelChoice{provider: provider, current: value.model, back: back,
		pick: func(m model, name string) (tea.Model, tea.Cmd) {
			if name != "" {
				value.provider, value.model = provider, name
			}
			return back(m)
		}})
}
