package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

const (
	effortsTimeout = 3 * time.Second
	// effortDefault names the empty effort, the model's own.
	effortDefault = "default"
	effortUnknown = "the provider does not say which efforts this model takes: every one is offered, and one it refuses is left out from then on"
)

// effortInfo is what the model in use takes: its efforts, whether the
// provider said so, and for which provider and model.
type effortInfo struct {
	key    string
	levels []string
	known  bool
	loaded bool
}

type effortsMsg effortInfo

func effortKey(session Session) string {
	return session.Provider + "/" + session.Model
}

// loadEfforts asks the provider which efforts the model takes, off the UI
// goroutine: for llama-server it reads the server.
func loadEfforts(session Session) tea.Cmd {
	if session.Agent == nil {
		return nil
	}
	lister, ok := session.Agent.Provider.(llm.EffortLister)
	key := effortKey(session)
	if !ok {
		return func() tea.Msg { return effortsMsg{key: key, levels: llm.AllEfforts, loaded: true} }
	}
	model := session.Model
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), effortsTimeout)
		defer cancel()
		levels, known := lister.Efforts(ctx, model)
		return effortsMsg{key: key, levels: levels, known: known, loaded: true}
	}
}

// currentEffort is the effort sent now, empty for the model's own.
func (m model) currentEffort() string {
	if m.session.Agent == nil {
		return ""
	}
	return m.session.Agent.Effort
}

// effortLevels are the efforts to offer for the model in use, those not
// known yet as every one.
func (m model) effortLevels() ([]string, bool) {
	if m.efforts.loaded && m.efforts.key == effortKey(m.session) {
		return m.efforts.levels, m.efforts.known
	}
	return llm.AllEfforts, false
}

// effortCommand sets the effort, or offers them all in a menu.
func (m model) effortCommand(argument string) (tea.Model, tea.Cmd) {
	levels, known := m.effortLevels()
	if argument == "" {
		if known && len(levels) == 0 {
			return m.emit(m.printer.Note(shortModelName(m.session.Model) + " takes no effort: it is sent with the model's own"))
		}
		var options []huh.Option[string]
		for _, level := range append([]string{effortDefault}, levels...) {
			label := level
			if level == effortDefault {
				label += ": the model's own"
			}
			if level == orDefault(m.currentEffort(), effortDefault) {
				label += "  ● now"
			}
			options = append(options, huh.NewOption(label, level))
		}
		description := "for " + shortModelName(m.session.Model) + " · saved like /model"
		if !known {
			description += "\n" + effortUnknown
		}
		return m.showChoices("Effort", description, options, func(m model, choice string) (tea.Model, tea.Cmd) {
			return m.setEffort(choice)
		})
	}
	if argument != effortDefault && !slices.Contains(levels, argument) && (known || !slices.Contains(llm.AllEfforts, argument)) {
		return m.emit(m.printer.Note(fmt.Sprintf("%s takes %s", shortModelName(m.session.Model), strings.Join(append([]string{effortDefault}, levels...), ", "))))
	}
	updated, cmd := m.setEffort(argument)
	if !known {
		shown, note := updated.(model).emit(m.printer.Note(effortUnknown))
		return shown, tea.Batch(cmd, note)
	}
	return updated, cmd
}

// setEffort saves an effort as /model saves a model: in the project file
// when it sets one, else in the user's.
func (m model) setEffort(level string) (tea.Model, tea.Cmd) {
	if level == effortDefault {
		level = ""
	}
	if level == m.currentEffort() {
		return m.emit(m.printer.Note("effort unchanged"))
	}
	layer, err := config.ReadUserLayer()
	if err != nil {
		return m.emitError(err)
	}
	if project, err := config.ReadProject(m.session.WorkDir); err == nil && config.ProjectSets(project.Layer)["effort"] {
		chosen := project.Layer
		chosen.Effort, chosen.Providers = level, nil
		keep := func(layer config.Config) (config.Config, error) { return layer, nil }
		return m.saveOrWait(&settingsScreen{layer: layer, apply: keep, project: &chosen, keepUser: true})
	}
	apply := func(layer config.Config) (config.Config, error) {
		layer.Effort = level
		return layer, nil
	}
	return m.saveOrWait(&settingsScreen{layer: layer, apply: apply})
}

// effortSuggestions are the efforts after /effort that start with what was
// typed, default first.
func (m model) effortSuggestions(partial string) []command {
	levels, known := m.effortLevels()
	var matches []command
	for _, level := range append([]string{effortDefault}, levels...) {
		if !strings.HasPrefix(level, partial) {
			continue
		}
		description := ""
		switch {
		case level == orDefault(m.currentEffort(), effortDefault):
			description = "now"
		case level == effortDefault:
			description = "the model's own"
		case !known:
			description = "not said by the provider"
		}
		matches = append(matches, command{name: "effort " + level, description: description})
	}
	return matches
}
