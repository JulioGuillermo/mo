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
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/tools"
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
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mo [-p provider] [-m model] [-y] [prompt]")
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

	chosen, err := chooseProvider(ctx, cfg, *providerFlag, *modelFlag)
	if err != nil {
		return err
	}
	confirm := cfg.Tools.Confirm
	if *yesFlag {
		confirm = nil
	}
	env := toolEnv(workDir, cfg.Memory.Dir)
	registry := tools.NewRegistry(env, tools.Options{
		Mode:         cfg.Tools.Mode,
		AlwaysLoaded: cfg.Tools.AlwaysLoaded,
		Disabled:     cfg.Tools.Disabled,
		MaxOutput:    cfg.Tools.MaxOutput,
	})
	memoryIndex := ""
	if !slices.Contains(cfg.Tools.Disabled, "memory") {
		memoryIndex = tools.MemoryIndex(env)
	}
	assistant := &agent.Agent{
		Provider:      chosen.provider,
		Model:         chosen.model,
		System:        agent.SystemPrompt(cfg.SystemPrompt, workDir, memoryIndex),
		MaxTokens:     cfg.MaxTokens,
		Tools:         registry,
		ConfirmTools:  confirm,
		Window:        chosen.window,
		CompactAt:     *cfg.Context.CompactAt,
		CompactPrompt: cfg.Context.CompactPrompt,
	}

	prompt, err := oneShotPrompt(flag.Args())
	if err != nil {
		return err
	}
	if prompt != "" {
		return runOnce(ctx, assistant, prompt)
	}
	stop()
	return tui.Run(tui.Session{Agent: assistant, Provider: chosen.name, Model: chosen.model})
}

func toolEnv(workDir, memoryDir string) tools.Env {
	return tools.Env{
		WorkDir:        workDir,
		ProtectedPaths: []string{filepath.Dir(config.UserFile()), config.ProjectFile(workDir), memoryDir},
		MemoryDir:      memoryDir,
	}
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
