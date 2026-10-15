package tui

import (
	"context"
	"errors"
	"strings"

	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
)

const (
	newProviderChoice = "\x00new"
	// activeProviderChoice opens the choice of the provider in use.
	activeProviderChoice = "\x00active"
	continueChoice       = "\x00continue"
	quitChoice           = "\x00quit"
	defaultNewURL        = "http://localhost:8080/v1"
)

// ProviderProblem is what the setup screen starts from: why mo cannot talk
// to a provider, the settings in effect, the user file and what was found.
type ProviderProblem struct {
	Err         error
	Config      config.Config
	Layer       config.Config
	Findings    []detect.Finding
	CanContinue bool
}

type SetupOutcome int

const (
	SetupUse SetupOutcome = iota
	SetupContinue
	SetupQuit
)

// providerSetup holds what the setup form edits: the choice, a key and a
// model for a provider mo knows, or a whole new one.
type providerSetup struct {
	choice   string
	key      string
	model    string
	provider *providerDraft
}

// AskProvider shows the problem and lets the user pick a provider mo knows
// or found, set up a new one, go on anyway or quit. With SetupUse it returns
// the user layer with the choice in it.
func AskProvider(ctx context.Context, problem ProviderProblem) (config.Config, SetupOutcome, error) {
	for {
		setup := &providerSetup{choice: problem.Layer.Provider, provider: newSetupProvider(problem.Config)}
		err := setupForm(problem, setup).RunWithContext(ctx)
		if errors.Is(err, huh.ErrUserAborted) {
			return problem.Layer, SetupQuit, nil
		}
		if err != nil {
			return problem.Layer, SetupQuit, err
		}
		switch setup.choice {
		case quitChoice:
			return problem.Layer, SetupQuit, nil
		case continueChoice:
			return problem.Layer, SetupContinue, nil
		}
		layer, err := setup.applyTo(problem.Layer, problem.Findings)
		if err == nil {
			return layer, SetupUse, nil
		}
		problem.Err = err
	}
}

func setupForm(problem ProviderProblem, setup *providerSetup) *huh.Form {
	known := func() bool {
		return setup.choice != newProviderChoice && setup.choice != continueChoice && setup.choice != quitChoice
	}
	return newForm(
		huh.NewGroup(
			huh.NewNote().Title("mo cannot talk to a provider").Description(problem.Err.Error()+"\n\nPick one, set up a new one, or quit."),
			huh.NewSelect[string]().Title("Provider").Options(setupOptions(problem)...).Value(&setup.choice),
		),
		huh.NewGroup(setup.provider.fields(problem.Config)...).Title("New provider").
			WithHideFunc(func() bool { return setup.choice != newProviderChoice }),
		huh.NewGroup(
			huh.NewInput().Title("API key").Description("saved in "+config.UserFile()+"; empty keeps the one it has").
				EchoMode(huh.EchoModePassword).Value(&setup.key),
		).WithHideFunc(func() bool { return !known() || !setup.needsKey(problem.Config) }),
		huh.NewGroup(
			huh.NewInput().Title("Model").Description("empty: the one configured, or the first the server lists").Value(&setup.model),
		).WithHideFunc(func() bool { return !known() }),
	)
}

// setupOptions lists the providers mo knows or found, then the ways out.
func setupOptions(problem ProviderProblem) []huh.Option[string] {
	options := providerOptions(problem.Config, problem.Findings, problem.Layer.Provider)
	options = append(options, huh.NewOption("+ set up a new provider", newProviderChoice))
	if problem.CanContinue {
		options = append(options, huh.NewOption("go on with "+providerName(problem.Config)+" anyway", continueChoice))
	}
	return append(options, huh.NewOption("quit", quitChoice))
}

func providerName(cfg config.Config) string {
	if cfg.Provider == "" {
		return "it"
	}
	return cfg.Provider
}

// newSetupProvider is a new provider that is used once saved, without asking.
func newSetupProvider(cfg config.Config) *providerDraft {
	draft := newProviderDraft(newProviderChoice, cfg, nil)
	draft.inUse = true
	return draft
}

func (s *providerSetup) needsKey(cfg config.Config) bool {
	settings, known := cfg.Providers[s.choice]
	return known && settings.NeedsKey()
}

// applyTo writes the choice into the user layer: a new provider whole, or
// the key and the model on a known one. A top-level model stays with the
// provider it was set for.
func (s *providerSetup) applyTo(layer config.Config, findings []detect.Finding) (config.Config, error) {
	if s.choice == newProviderChoice {
		return s.provider.applyTo(layer)
	}
	model := strings.TrimSpace(s.model)
	if s.choice != layer.Provider && model == "" {
		layer.Model = ""
	}
	choice := &settingsDraft{provider: s.choice, model: model, initialProvider: layer.Provider}
	layer = choice.applyProvider(layer, findings)
	if key := strings.TrimSpace(s.key); key != "" {
		entry := layer.Providers[s.choice]
		entry.APIKey = key
		layer.Providers = withEntry(layer.Providers, s.choice, entry)
	}
	return layer, nil
}

func required(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("required")
	}
	return nil
}
