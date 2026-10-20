package tui

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/jg/mo/internal/config"
)

// configChangedMsg says a settings file changed on disk.
type configChangedMsg struct{}

// configBuiltMsg is a session built from the files on disk; whole when
// /reload asked for it, provider and model included.
type configBuiltMsg struct {
	session Session
	whole   bool
	err     error
}

const configRetry = time.Second

// configSync follows the settings files: the ones last applied, and the
// project file the user answered about when it was not trusted.
type configSync struct {
	watcher *config.Watcher
	applied config.Files
	// answeredProject is the project file asked about; keptProject is set
	// when the user chose to ignore it, the one to go on with.
	answeredProject string
	keptProject     *config.Project
}

// watchConfig follows the settings files of the session's folder.
func watchConfig(workDir string) *configSync {
	sync := &configSync{}
	sync.applied, _ = config.ReadFiles(workDir)
	sync.answeredProject = sync.applied.Project.Digest()
	if watcher, err := config.Watch(workDir); err == nil {
		sync.watcher = watcher
	}
	return sync
}

// tell sends the program a message each time the files change.
func (s *configSync) tell(bridge *bridge) {
	if s.watcher == nil {
		return
	}
	go func() {
		for range s.watcher.Changes() {
			bridge.send(configChangedMsg{})
		}
	}()
}

func (s *configSync) close() {
	if s.watcher != nil {
		s.watcher.Close()
	}
}

// checkConfig looks at the files again, for a change the system did not
// report.
func (m model) checkConfig() {
	if m.config != nil && m.config.watcher != nil {
		m.config.watcher.Check()
	}
}

// configChanged follows the files on disk: what only the screen uses at
// once, the rest once the model ends its turn; provider, model, effort and
// permission mode wait for /reload.
func (m model) configChanged() (tea.Model, tea.Cmd) {
	if m.settings != nil || m.settingsInFlight {
		return m, tea.Tick(configRetry, func(time.Time) tea.Msg { return configChangedMsg{} })
	}
	return m.followConfig(false)
}

// followConfig reads the files and puts them in force, asking first when
// one is broken or the project file changed and is not trusted.
func (m model) followConfig(whole bool) (tea.Model, tea.Cmd) {
	if m.config == nil {
		return m, nil
	}
	files, err := config.ReadFiles(m.session.WorkDir)
	var broken *config.BrokenFile
	if errors.As(err, &broken) {
		return m.showBrokenFile(broken, whole)
	}
	if m.popup != nil && m.popup.config {
		m.popup = nil
	}
	digest := files.Project.Digest()
	if held := files.Project.HeldBackKeys(m.session.WorkDir, files.User); len(held) > 0 && digest != m.config.answeredProject {
		return m.askTrust(files, held, whole)
	}
	if digest != m.config.answeredProject {
		m.config.answeredProject, m.config.keptProject = digest, nil
	}
	if kept := m.config.keptProject; kept != nil {
		files.Project = *kept
		files.Settings, err = config.FromFiles(m.session.WorkDir, files.User, *kept)
		if err != nil {
			return m.emitError(err)
		}
	}
	return m.applyFiles(files, whole)
}

// applyFiles puts in force what the files set: the screen's own settings
// now, the session's once built.
func (m model) applyFiles(files config.Files, whole bool) (tea.Model, tea.Cmd) {
	previous := m.config.applied.Settings
	m.config.applied = files
	disk := files.Settings
	current := m.session
	if m.pendingSession != nil {
		current = *m.pendingSession
		m.pendingSession.Config.Render, m.pendingSession.Config.Notify = disk.Render, disk.Notify
	}
	uiChanged := !reflect.DeepEqual(disk.Render, m.session.Config.Render) || !reflect.DeepEqual(disk.Notify, m.session.Config.Notify)
	m.session.Config.Render, m.session.Config.Notify = disk.Render, disk.Notify
	current.Config.Render, current.Config.Notify = disk.Render, disk.Notify
	var cmds []tea.Cmd
	if uiChanged {
		cmds = append(cmds, m.applyUI())
	}

	if !whole {
		changedOnDisk := !reflect.DeepEqual(sessionPart(disk), sessionPart(previous))
		if changedOnDisk && !reflect.DeepEqual(sessionPart(disk), sessionPart(current.Config)) {
			cmds = append(cmds, printText(m.printer.Note("settings changed on disk · provider, model, effort and permission mode apply with /reload")))
		}
		disk = keepSessionSettings(disk, current)
		if reflect.DeepEqual(disk, keepSessionSettings(withoutPluginProviders(current.Config), current)) {
			if uiChanged {
				cmds = append(cmds, printText(m.printer.Note("settings changed on disk · the look applies now")))
			}
			return m, tea.Batch(cmds...)
		}
	}
	m.settingsInFlight = true
	cmds = append(cmds, rebuildSession(current, disk, whole))
	return m, tea.Batch(cmds...)
}

func rebuildSession(current Session, cfg config.Config, whole bool) tea.Cmd {
	return func() tea.Msg {
		if current.Rebuild == nil {
			return configBuiltMsg{err: errors.New("this session cannot load settings again")}
		}
		session, err := current.Rebuild(cfg)
		return configBuiltMsg{session: session, whole: whole, err: err}
	}
}

// configBuilt puts the session built from disk in place, once the model
// ends its turn if it is working.
func (m model) configBuilt(msg configBuiltMsg) (tea.Model, tea.Cmd) {
	m.settingsInFlight = false
	if msg.err != nil {
		return m.emitError(fmt.Errorf("settings on disk not applied: %w", msg.err))
	}
	if m.running {
		if m.pendingSession != nil {
			m.pendingSession.discard()
		}
		m.pendingSession = &msg.session
		return m.emit(m.printer.Note("settings changed on disk · they apply when the model ends this turn"))
	}
	takeOver(msg.session, m.session)
	updated, cmd := m.useSession(msg.session)
	note := "settings changed on disk · applied"
	if msg.whole {
		note = fmt.Sprintf("settings loaded again · %s · %s", msg.session.Provider, shortModelName(msg.session.Model))
	}
	return updated, tea.Batch(cmd, printText(m.printer.Note(note)))
}

// showBrokenFile keeps the settings in force and asks what to do with the
// file: leave it to be fixed, or put back the last one that worked or the
// defaults.
func (m model) showBrokenFile(broken *config.BrokenFile, whole bool) (tea.Model, tea.Cmd) {
	options := []huh.Option[string]{
		huh.NewOption("Accept · keep going with the settings in force", "accept"),
		huh.NewOption("Restore the settings in force", "restore"),
		huh.NewOption("Restore the defaults", "defaults"),
	}
	description := fmt.Sprintf("%v\n\nmo keeps the settings in force. Restoring keeps this file as %s.broken.", broken.Err, broken.Path)
	applied := m.config.applied
	m.popup = &popup{
		title:       "✗ settings file broken: " + broken.Path,
		description: description,
		options:     options,
		border:      popupError,
		config:      true,
		do: func(m model, choice string) (tea.Model, tea.Cmd) {
			var err error
			switch choice {
			case "accept":
				return m, nil
			case "restore":
				err = applied.Restore(broken.Path)
			case "defaults":
				err = config.ResetFile(broken.Path)
			}
			if err != nil {
				return m.emitError(err)
			}
			return m.followConfig(whole)
		},
	}
	return m, nil
}

// askTrust asks about a project file changed outside mo that sets more
// than applies untrusted.
func (m model) askTrust(files config.Files, held []string, whole bool) (tea.Model, tea.Cmd) {
	options := []huh.Option[string]{
		huh.NewOption("Trust it and load it all", "trust"),
		huh.NewOption("Load only its harmless part", "load"),
		huh.NewOption("Ignore it · keep the settings in force", "ignore"),
	}
	description := fmt.Sprintf("%s changed outside mo. These settings apply only if you trust it: %s.", files.Project.Path, strings.Join(held, ", "))
	kept := m.config.applied.Project
	digest := files.Project.Digest()
	m.popup = &popup{
		title:       "! project settings changed",
		description: description,
		options:     options,
		border:      warning,
		config:      true,
		do: func(m model, choice string) (tea.Model, tea.Cmd) {
			m.config.answeredProject, m.config.keptProject = digest, nil
			switch choice {
			case "trust":
				if err := files.Project.Trust(); err != nil {
					return m.emitError(err)
				}
			case "ignore":
				m.config.keptProject = &kept
			}
			return m.followConfig(whole)
		},
	}
	return m, nil
}

// sessionPart is what of the settings changes only with /reload.
// withoutPluginProviders is cfg without the providers plugins declared,
// which are in the session's settings and never in a file.
func withoutPluginProviders(cfg config.Config) config.Config {
	cfg.Providers = maps.Clone(cfg.Providers)
	maps.DeleteFunc(cfg.Providers, func(_ string, provider config.ProviderConfig) bool { return provider.Plugin != "" })
	return cfg
}

func sessionPart(cfg config.Config) config.Config {
	return config.Config{
		Provider:    cfg.Provider,
		Model:       config.EffectiveModel(cfg),
		Effort:      cfg.Effort,
		MaxTokens:   cfg.MaxTokens,
		Permissions: config.Permissions{Mode: cfg.Permissions.Mode},
		Providers:   map[string]config.ProviderConfig{cfg.Provider: cfg.Providers[cfg.Provider]},
	}
}

// keepSessionSettings is the settings read from disk with the session's
// provider, model, effort and permission mode.
func keepSessionSettings(disk config.Config, session Session) config.Config {
	disk.Provider = session.Provider
	disk.Model = session.Model
	disk.Effort = session.Config.Effort
	disk.MaxTokens = session.Config.MaxTokens
	disk.Permissions.Mode = session.Config.Permissions.Mode
	disk.Providers = maps.Clone(disk.Providers)
	if provider, found := session.Config.Providers[session.Provider]; found {
		if disk.Providers == nil {
			disk.Providers = map[string]config.ProviderConfig{}
		}
		disk.Providers[session.Provider] = provider
	}
	return disk
}
