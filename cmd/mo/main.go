// mo is a minimal coding agent for the terminal.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/peers"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/team"
	"github.com/jg/mo/internal/tools"
	"github.com/jg/mo/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mo:", err)
		os.Exit(1)
	}
}

// keepCrashes writes what the Go runtime prints when mo crashes to a file
// too: the screen may not keep it, and the terminal is left in raw mode.
func keepCrashes() {
	if err := os.MkdirAll(config.LogDir(), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(config.LogDir(), "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_ = debug.SetCrashOutput(file, debug.CrashOptions{})
	file.Close()
}

func run() error {
	keepCrashes()
	providerFlag := flag.String("p", "", "provider name, e.g. ollama, llama-server, anthropic")
	modelFlag := flag.String("m", "", "model name")
	yesFlag := flag.Bool("y", false, "run tools without asking, except changes outside the working directory")
	detectFlag := flag.Bool("detect", false, "list the providers mo can find and exit")
	continueFlag := flag.Bool("c", false, "continue the latest conversation of this folder")
	resumeFlag := flag.Bool("resume", false, "open on the list of this folder's conversations, to pick one; -r too")
	flag.BoolVar(resumeFlag, "r", false, "the same as -resume")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mo [-p provider] [-m model] [-y] [-c | -r] [-detect] [prompt]")
		fmt.Fprintln(os.Stderr, "With a prompt, or with input on stdin, mo answers once and exits.")
		flag.PrintDefaults()
	}
	flag.Parse()

	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(workDir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *detectFlag {
		printFindings(detect.Run(ctx, cfg))
		return nil
	}

	loadedPlugins := &openPlugins{current: loadPlugins(workDir, cfg.Plugins)}
	defer loadedPlugins.Close()
	hubs := &openHubs{}
	servers := activeServers(cfg, loadedPlugins.current)
	connecting := connectInBackground(func() *mcphub.Hub { return connectMCP(ctx, servers, loadedPlugins.current.MCPHeaders) }, hubs)
	defer hubs.Close()
	// What never took the connection still has to close it.
	defer func() { connecting.get() }()
	defer render.RemoveSavedImages()
	choice := sessionChoice{provider: *providerFlag, model: *modelFlag, yes: *yesFlag}
	loaded := extensions{connecting: connecting, plugins: loadedPlugins.current, pluginSets: loadedPlugins, hubs: hubs, recall: tools.NewRecall(config.SessionDir())}

	prompt, err := oneShotPrompt(flag.Args())
	if err != nil {
		return err
	}
	if prompt != "" {
		loaded.team = team.New()
		defer loaded.team.Close()
		session, err := buildSession(ctx, workDir, cfg, choice, loaded)
		if err != nil {
			return err
		}
		followOnce(loaded.team)
		return runOnce(ctx, session.Agent, prompt)
	}
	defer startJobs(&loaded)()
	session, err := startSession(ctx, workDir, cfg, choice, loaded, tui.AskProvider)
	if err != nil {
		return err
	}
	stop()
	session.Continue = *continueFlag
	session.Resume = *resumeFlag && !*continueFlag
	return tui.Run(session)
}

// startJobs gives the interactive session its background jobs, killed when
// mo exits, the title tool, which only it can take, and its subagents.
func startJobs(loaded *extensions) func() {
	loaded.jobs = tools.NewJobs(config.SessionDir())
	loaded.titles = tools.NewTitles()
	loaded.uis = tools.NewUIs(config.SessionDir())
	loaded.team = team.New()
	loaded.peers = peers.NewNode(peers.SocketDir())
	return func() {
		loaded.peers.Stop()
		loaded.team.Close()
		loaded.jobs.Close()
	}
}

// connectMCP connects to the servers before the UI starts and reports the
// ones that failed. A settings change connects again only those that
// changed.
func connectMCP(ctx context.Context, servers map[string]config.MCPServer, headers mcphub.PluginHeaders) *mcphub.Hub {
	if len(servers) == 0 {
		return mcphub.Connect(ctx, nil, "", nil)
	}
	fmt.Fprintf(os.Stderr, "connecting to %d MCP server(s)…\n", len(servers))
	hub := mcphub.Connect(ctx, servers, config.LogDir(), headers)
	for _, status := range hub.Status() {
		if status.Err != nil {
			fmt.Fprintf(os.Stderr, "mo: MCP %s: %v\n", status.Name, status.Err)
		}
	}
	return hub
}

// oneShotPrompt returns the prompt from the arguments, or from stdin when it
// is piped in. Empty means interactive mode.
func oneShotPrompt(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if term.IsTerminal(os.Stdin.Fd()) {
		return "", nil
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(input)), nil
}
