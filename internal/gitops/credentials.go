package gitops

import (
	"bufio"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/config"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

const helperTimeout = 20 * time.Second

// helpers are the credential helpers git's config names for a URL, the
// user's config then the repository's: those of a credential.<url> section
// that matches it, then the general ones. An empty helper resets the list,
// as in git.
func (r *Repo) helpers(remote *url.URL) []string {
	var sections []*format.Section
	if global, err := config.LoadConfig(config.GlobalScope); err == nil && global.Raw != nil {
		sections = append(sections, global.Raw.Section("credential"))
	}
	if local, err := r.repo.Config(); err == nil && local.Raw != nil {
		sections = append(sections, local.Raw.Section("credential"))
	}
	var names []string
	for _, section := range sections {
		for _, sub := range section.Subsections {
			if prefix, err := url.Parse(sub.Name); err == nil && prefix.Host == remote.Host && strings.HasPrefix(remote.Path, prefix.Path) {
				names = append(names, sub.OptionAll("helper")...)
			}
		}
		names = append(names, section.OptionAll("helper")...)
	}
	var helpers []string
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			helpers = nil
			continue
		}
		helpers = append(helpers, name)
	}
	return helpers
}

// helperAuth asks git's credential helpers for a URL, the way git does, so
// a login kept by Git Credential Manager, libsecret, the macOS keychain or
// git's store file works without the git command. The cache helper needs
// git's own daemon and is left to it.
func (r *Repo) helperAuth(raw string) *http.BasicAuth {
	remote, err := url.Parse(raw)
	if err != nil || (remote.Scheme != "https" && remote.Scheme != "http") {
		return nil
	}
	for _, helper := range r.helpers(remote) {
		user, password := askHelper(helper, remote)
		if password != "" {
			return &http.BasicAuth{Username: orDefault(user, "x-access-token"), Password: password}
		}
	}
	return nil
}

func askHelper(helper string, remote *url.URL) (string, string) {
	name, options, _ := strings.Cut(strings.TrimSpace(helper), " ")
	switch {
	case name == "cache":
		return "", ""
	case name == "store":
		return fromStore(options, remote)
	}
	var command *exec.Cmd
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()
	switch {
	case strings.HasPrefix(helper, "!"):
		command = exec.CommandContext(ctx, "sh", "-c", strings.TrimPrefix(helper, "!")+" get")
	case filepath.IsAbs(name):
		command = exec.CommandContext(ctx, name, append(strings.Fields(options), "get")...)
	default:
		program, err := exec.LookPath("git-credential-" + name)
		if err != nil {
			return "", ""
		}
		command = exec.CommandContext(ctx, program, append(strings.Fields(options), "get")...)
	}
	request := "protocol=" + remote.Scheme + "\nhost=" + remote.Host + "\n"
	if user := remote.User.Username(); user != "" {
		request += "username=" + user + "\n"
	}
	command.Stdin = strings.NewReader(request + "\n")
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.Output()
	if err != nil {
		return "", ""
	}
	return readCredential(string(output))
}

func readCredential(output string) (user, password string) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), "=")
		switch key {
		case "username":
			user = value
		case "password":
			password = value
		}
	}
	return user, password
}

// fromStore reads git's store helper file, ~/.git-credentials or the one
// --file names, for a login to the URL's host.
func fromStore(options string, remote *url.URL) (string, string) {
	home, _ := os.UserHomeDir()
	paths := []string{filepath.Join(home, ".git-credentials")}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "git", "credentials"))
	}
	if file, found := strings.CutPrefix(strings.TrimSpace(options), "--file="); found {
		paths = []string{strings.Trim(file, `"'`)}
	} else if file, found := strings.CutPrefix(strings.TrimSpace(options), "--file "); found {
		paths = []string{strings.Trim(strings.TrimSpace(file), `"'`)}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			saved, err := url.Parse(strings.TrimSpace(line))
			if err != nil || saved.Host != remote.Host || saved.Scheme != remote.Scheme || saved.User == nil {
				continue
			}
			if want := remote.User.Username(); want != "" && want != saved.User.Username() {
				continue
			}
			password, _ := saved.User.Password()
			return saved.User.Username(), password
		}
	}
	return "", ""
}
