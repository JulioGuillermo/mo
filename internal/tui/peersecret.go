package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/peers"
)

const (
	secretGlobal  = "global"
	secretProject = "project"
	secretSession = "session"
	secretUsage   = "/peers secret · new [global|project|session] · set global|project|session <secret> · clear global|project|session"
)

// peerSecret is the network's secret in force: the session's, else the
// folder's, else the global one.
func (m model) peerSecret() string {
	if m.peers.secret != "" {
		return m.peers.secret
	}
	return m.session.Config.Peers.Key()
}

// secretCommand is /peers secret: where the one in force comes from, or a
// new one, one set or one taken away, globally, for this folder, or for
// the session alone.
func (m model) secretCommand(argument string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(argument)
	if len(fields) == 0 {
		return m.emit(m.printer.Note("secret: " + m.secretSource() + " · " + secretUsage))
	}
	action, scope, secret := fields[0], secretGlobal, ""
	if len(fields) > 1 {
		scope = fields[1]
	}
	if len(fields) > 2 {
		secret = strings.Join(fields[2:], " ")
	}
	if scope != secretGlobal && scope != secretProject && scope != secretSession {
		return m.emit(m.printer.Note("use global, project or session · " + secretUsage))
	}
	shown := false
	switch action {
	case "new":
		secret, shown = peers.NewSecret(), true
	case "set":
		if err := validSecret(secret); err != nil || secret == "" {
			return m.emit(m.printer.Note(fmt.Sprintf("the secret goes after the scope, %d characters or more · %s", config.PeerSecretMin, secretUsage)))
		}
	case "clear":
		secret = ""
	default:
		return m.emit(m.printer.Note(secretUsage))
	}
	inForce, err := m.keepSecret(scope, secret)
	if err != nil {
		return m.emitError(err)
	}
	m.session.Config.Peers.Secret, m.session.Config.Peers.SecretEnv = inForce.Secret, inForce.SecretEnv
	note := m.secretNote(action, scope, secret, shown)
	applied := m.applyNetwork()
	done, show := m.emit(m.printer.Note(note))
	return done, tea.Batch(applied, show)
}

// keepSecret puts a secret, or none, where scope says: the session, or the
// user file, globally or for this folder; it gives the settings' secret in
// force then.
func (m model) keepSecret(scope, secret string) (config.Peers, error) {
	if scope == secretSession {
		m.peers.secret = secret
		return m.session.Config.Peers, nil
	}
	layer, err := config.ReadUserLayer()
	if err != nil {
		return config.Peers{}, err
	}
	if scope == secretGlobal {
		layer.Peers.Secret, layer.Peers.SecretEnv = secret, ""
	} else {
		folder := config.ProjectKey(m.session.WorkDir)
		if secret == "" {
			layer.Peers.Projects = withoutEntry(layer.Peers.Projects, folder)
		} else {
			layer.Peers.Projects = withEntry(layer.Peers.Projects, folder, config.PeerProject{Secret: secret})
		}
		if len(layer.Peers.Projects) == 0 {
			layer.Peers.Projects = nil
		}
	}
	if err := config.SaveUserLayer(layer); err != nil {
		return config.Peers{}, err
	}
	return config.UserSettings(layer).Peers.ForProject(m.session.WorkDir), nil
}

// secretNote says what was done, and, for a new secret, the secret to set
// on the other computers: on the screen only, never in the conversation
// the model reads.
func (m model) secretNote(action, scope, secret string, shown bool) string {
	where := map[string]string{
		secretGlobal:  "globally, in " + config.UserFile(),
		secretProject: "for this folder, in " + config.UserFile(),
		secretSession: "for this session alone, kept nowhere",
	}[scope]
	note := "secret " + map[string]string{"new": "made", "set": "set", "clear": "cleared"}[action] + " " + where
	if shown {
		note += ":\n  " + secret + "\nset it on the other computers of the team"
	}
	if action != "clear" && m.secretSource() != scope {
		note += "\nthe " + m.secretSource() + " one still applies here"
	}
	return note + "\nsecret in force: " + m.secretSource()
}

// secretSource says where the secret in force comes from, never what it is.
func (m model) secretSource() string {
	settings := m.session.Config.Peers
	switch {
	case m.peers.secret != "":
		return secretSession
	case settings.Key() == "":
		return "none"
	}
	layer, err := config.ReadUserLayer()
	if err == nil {
		if own, found := layer.Peers.Projects[config.ProjectKey(m.session.WorkDir)]; found && (own.Secret != "" || own.SecretEnv != "") {
			return secretProject
		}
	}
	if settings.Secret == "" && settings.SecretEnv != "" {
		return "global, from $" + settings.SecretEnv
	}
	return secretGlobal
}

// keptInHistory tells whether a command typed may go to the input's
// history on disk: never one that holds a secret.
func keptInHistory(entry string) bool {
	fields := strings.Fields(entry)
	return len(fields) < 3 || fields[0] != "/peers" || fields[1] != "secret" || fields[2] != "set"
}
