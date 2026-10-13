package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/tui"
)

const (
	reachTimeout       = 3 * time.Second
	setupDetectTimeout = 3 * time.Second
)

var errNoProvider = errors.New("no provider chosen; mo -detect lists what mo can find")

// providerError is a session that could not be built for its provider.
type providerError struct{ err error }

func (e providerError) Error() string { return e.err.Error() }
func (e providerError) Unwrap() error { return e.err }

type askProvider func(ctx context.Context, problem tui.ProviderProblem) (config.Config, tui.SetupOutcome, error)

// startSession builds the session for the interactive mode. While there is
// no provider mo can reach, it asks for one, tries it, and saves it to the
// user file once it works, or once the user goes on with one that was built.
// Every round starts from the last choice that was built.
func startSession(ctx context.Context, workDir string, cfg config.Config, choice sessionChoice, loaded extensions, ask askProvider) (tui.Session, error) {
	session, err := buildSession(ctx, workDir, cfg, choice, loaded)
	if err != nil && !errors.As(err, new(providerError)) {
		return tui.Session{}, err
	}
	var fallback *tui.Session
	if err == nil {
		fallback = &session
		err = reachable(ctx, session)
	}
	if err == nil {
		return session, nil
	}
	base, readErr := config.ReadUserLayer()
	if readErr != nil {
		return tui.Session{}, readErr
	}
	var baseChanged bool
	for {
		fmt.Fprintln(os.Stderr, "looking for providers…")
		problem := tui.ProviderProblem{Err: err, Config: cfg, Layer: base, Findings: findProviders(ctx, cfg), CanContinue: fallback != nil}
		picked, outcome, askErr := ask(ctx, problem)
		switch {
		case askErr != nil:
			return tui.Session{}, askErr
		case outcome == tui.SetupQuit:
			return tui.Session{}, errNoProvider
		case outcome == tui.SetupContinue:
			return *fallback, saveSetup(base, baseChanged)
		}
		tried, loadErr := config.LoadWithUserLayer(workDir, picked)
		if loadErr != nil {
			err = loadErr
			continue
		}
		candidate, buildErr := buildSession(ctx, workDir, tried, sessionChoice{yes: choice.yes}, loaded)
		if buildErr != nil {
			err = buildErr
			continue
		}
		fmt.Fprintf(os.Stderr, "trying %s…\n", candidate.Provider)
		if err = reachable(ctx, candidate); err == nil {
			return candidate, saveSetup(picked, true)
		}
		fallback, base, baseChanged, cfg = &candidate, picked, true, tried
	}
}

func saveSetup(layer config.Config, changed bool) error {
	if !changed {
		return nil
	}
	if err := config.SaveUserLayer(layer); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "provider saved to %s\n", config.UserFile())
	return nil
}

func findProviders(ctx context.Context, cfg config.Config) []detect.Finding {
	ctx, cancel := context.WithTimeout(ctx, setupDetectTimeout)
	defer cancel()
	return detect.Run(ctx, cfg)
}

// reachable asks an OpenAI-compatible server for its models. A server that
// has no model list still answers. An Anthropic key is not checked here.
func reachable(ctx context.Context, session tui.Session) error {
	server, ok := session.Agent.Provider.(*llm.OpenAI)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	_, err := server.ListModels(ctx)
	var status *llm.StatusError
	if err == nil || errors.As(err, &status) && status.Code == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("%s at %s does not answer: %w", session.Provider, server.BaseURL, err)
}
