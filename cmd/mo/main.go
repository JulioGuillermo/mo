// mo is a minimal coding agent for the terminal.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/detect"
	"github.com/jg/mo/internal/mcphub"
	"github.com/jg/mo/internal/render"
	"github.com/jg/mo/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mo:", err)
		os.Exit(1)
	}
}

func run() error {
	providerFlag := flag.String("p", "", "provider name, e.g. ollama, llama-server, anthropic")
	modelFlag := flag.String("m", "", "model name")
	yesFlag := flag.Bool("y", false, "run tools without asking, except changes outside the working directory")
	detectFlag := flag.Bool("detect", false, "list the providers mo can find and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mo [-p provider] [-m model] [-y] [-detect] [prompt]")
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

	hub := connectMCP(ctx, cfg)
	defer hub.Close()
	defer render.RemoveSavedImages()
	choice := sessionChoice{provider: *providerFlag, model: *modelFlag, yes: *yesFlag}
	session, err := buildSession(ctx, workDir, cfg, choice, hub)
	if err != nil {
		return err
	}

	prompt, err := oneShotPrompt(flag.Args())
	if err != nil {
		return err
	}
	if prompt != "" {
		return runOnce(ctx, session.Agent, prompt)
	}
	stop()
	return tui.Run(session)
}

// connectMCP connects to the configured servers before the UI starts and
// reports the ones that failed. They are connected once per run: settings
// changes keep the same servers.
func connectMCP(ctx context.Context, cfg config.Config) *mcphub.Hub {
	if len(cfg.MCP) == 0 {
		return mcphub.Connect(ctx, nil, "")
	}
	fmt.Fprintf(os.Stderr, "connecting to %d MCP server(s)…\n", len(cfg.MCP))
	hub := mcphub.Connect(ctx, cfg.MCP, config.LogDir())
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
