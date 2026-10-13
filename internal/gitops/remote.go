package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

const (
	defaultRemote  = "origin"
	networkTimeout = 2 * time.Minute
)

// inProcess serves remotes that are folders on this machine with go-git's
// own server when the git command is missing: go-git otherwise runs
// git-upload-pack for them. Its server can leave objects out of a fetch
// that is not a fast-forward, so git's is kept wherever it is installed.
// Remotes over SSH and HTTPS never need either.
var inProcess sync.Once

func serveLocalRemotes() {
	inProcess.Do(func() {
		if _, err := exec.LookPath("git-upload-pack"); err != nil {
			client.InstallProtocol("file", server.DefaultServer)
		}
	})
}

// tokenVariables hold an HTTPS token, checked in this order.
var tokenVariables = []string{"GIT_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN"}

// auth finds credentials for a remote URL the way a user has them without
// git's helpers: an SSH agent or a key without passphrase for SSH, a token
// from the environment for HTTPS. Nil when none is found.
func auth(url string) transport.AuthMethod {
	endpoint, err := transport.NewEndpoint(url)
	if err != nil {
		return nil
	}
	switch endpoint.Protocol {
	case "ssh":
		user := endpoint.User
		if user == "" {
			user = "git"
		}
		if os.Getenv("SSH_AUTH_SOCK") != "" {
			if agent, err := ssh.NewSSHAgentAuth(user); err == nil {
				return agent
			}
		}
		home, _ := os.UserHomeDir()
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			if keys, err := ssh.NewPublicKeysFromFile(user, filepath.Join(home, ".ssh", name), ""); err == nil {
				return keys
			}
		}
	case "http", "https":
		for _, variable := range tokenVariables {
			if token := os.Getenv(variable); token != "" {
				return &http.BasicAuth{Username: "x-access-token", Password: token}
			}
		}
	}
	return nil
}

func (r *Repo) remoteURL(name string) (string, error) {
	remote, err := r.repo.Remote(name)
	if err != nil {
		return "", fmt.Errorf("no remote %q", name)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("remote %q has no URL", name)
	}
	return urls[0], nil
}

// needsGit tells whether an error means go-git could not get in where the
// git command, with its credential helpers, may.
func needsGit(err error) bool {
	return errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed) ||
		errors.Is(err, transport.ErrInvalidAuthMethod) || strings.Contains(strings.ToLower(fmt.Sprint(err)), "auth")
}

// withGit runs the git command instead, when it is installed.
func (r *Repo) withGit(cause error, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("%w; set GIT_TOKEN for HTTPS, or load an SSH key in the agent", cause)
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", r.root}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
	}
	return "(through the git command) " + orNothing(text), nil
}

func orNothing(text string) string {
	if text == "" {
		return "done"
	}
	return text
}

// Fetch brings a remote's branches and tags.
func (r *Repo) Fetch(remote string) (string, error) {
	remote = orDefault(remote, defaultRemote)
	url, err := r.remoteURL(remote)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	err = r.repo.FetchContext(ctx, &git.FetchOptions{RemoteName: remote, Auth: auth(url), Tags: git.AllTags})
	switch {
	case errors.Is(err, git.NoErrAlreadyUpToDate):
		return "already up to date with " + remote, nil
	case err != nil && needsGit(err):
		return r.withGit(err, "fetch", remote)
	case err != nil:
		return "", err
	}
	return "fetched " + remote, nil
}

// Pull fetches the branch's remote and merges it in with the three-way
// merge, so a pull that is not a fast-forward works and its conflicts stop
// it like any merge.
func (r *Repo) Pull(remote string) (string, error) {
	remote = orDefault(remote, defaultRemote)
	if err := r.requireClean(); err != nil {
		return "", err
	}
	_, branch, err := r.head()
	if err != nil {
		return "", err
	}
	fetched, err := r.Fetch(remote)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(fetched, "(through the git command)") {
		return r.withGit(errors.New("pull"), "merge", remote+"/"+branch)
	}
	tracking := plumbing.NewRemoteReferenceName(remote, branch)
	if _, err := r.repo.Reference(tracking, true); err != nil {
		return "", fmt.Errorf("%s has no branch %s", remote, branch)
	}
	merged, err := r.Merge(tracking.String())
	if err != nil {
		return "", err
	}
	return fetched + "; " + merged, nil
}

// Push sends the current branch to the remote's branch of the same name.
// It never forces: a rejected push says to pull first.
func (r *Repo) Push(remote string) (string, error) {
	remote = orDefault(remote, defaultRemote)
	if r.inProgress() != nil {
		return "", errors.New("finish or abort what is in progress first")
	}
	ref, err := r.repo.Head()
	if err != nil || !ref.Name().IsBranch() {
		return "", errors.New("push needs a branch checked out")
	}
	url, err := r.remoteURL(remote)
	if err != nil {
		return "", err
	}
	spec := config.RefSpec(ref.Name().String() + ":" + ref.Name().String())
	ctx, cancel := context.WithTimeout(context.Background(), networkTimeout)
	defer cancel()
	err = r.repo.PushContext(ctx, &git.PushOptions{RemoteName: remote, Auth: auth(url), RefSpecs: []config.RefSpec{spec}})
	switch {
	case errors.Is(err, git.NoErrAlreadyUpToDate):
		return remote + " already has " + ref.Name().Short(), nil
	case err != nil && strings.Contains(err.Error(), "non-fast-forward"):
		return "", fmt.Errorf("%s has commits this branch lacks: pull first, then push", remote)
	case err != nil && needsGit(err):
		return r.withGit(err, "push", remote, ref.Name().Short())
	case err != nil:
		return "", err
	}
	return fmt.Sprintf("pushed %s to %s", ref.Name().Short(), remote), nil
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
