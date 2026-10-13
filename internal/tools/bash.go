package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const (
	bashDefaultTimeout = 120 * time.Second
	bashMaxTimeout     = 600 * time.Second
	bashKeptOutput     = 1 << 20
	bashWaitForOutput  = 2 * time.Second
)

func bashTool() Tool {
	return Tool{
		Name:        "bash",
		Description: "Run a bash command in the working directory. No stdin. background: run it as a job, for servers, watchers and long builds; you are told when it ends.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"command":{"type":"string"},
"timeout":{"type":"integer","description":"seconds, default 120"},
"background":{"type":"boolean"}},"required":["command"]}`),
		Run:     runBash,
		Summary: func(args json.RawMessage) string { return argField(args, "command") },
	}
}

func runBash(ctx context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Command    string `json:"command"`
		Timeout    int    `json:"timeout"`
		Background bool   `json:"background"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Background {
		if env.Jobs == nil {
			return "", errors.New(jobsUnavailable)
		}
		status, err := env.Jobs.Start(env, args.Command)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(jobStartedResult, status.ID, status.ID), nil
	}
	timeout := bashDefaultTimeout
	if args.Timeout > 0 {
		timeout = min(time.Duration(args.Timeout)*time.Second, bashMaxTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
	cmd.Dir = env.WorkDir
	output := NewHeadTailBuffer(bashKeptOutput)
	cmd.Stdout = output
	cmd.Stderr = output
	detachFromTerminal(cmd)

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return output.String(), fmt.Errorf("timed out after %s", timeout)
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return output.String() + "\n(output of background processes not captured)", nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return output.String(), fmt.Errorf("exit status %d", exitErr.ExitCode())
	}
	if err != nil {
		return output.String(), err
	}
	if output.String() == "" {
		return "(no output)", nil
	}
	return output.String(), nil
}

// detachFromTerminal runs the command in its own session, without a
// controlling terminal, so a password prompt fails instead of waiting, and a
// cancel kills everything the command started.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = bashWaitForOutput
}
