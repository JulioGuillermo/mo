package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	bashDefaultTimeout = 120 * time.Second
	bashMaxTimeout     = 600 * time.Second
	bashKeptOutput     = 1 << 20
	bashWaitForOutput  = 2 * time.Second
	bashUncaptured     = "(output of background processes not captured)"
	bashDefaultWait    = 30 * time.Second
	bashWaitPoll       = 100 * time.Millisecond
)

var bashHelp = []helpTopic{
	{"run", "the default: command runs in the working directory, without stdin. timeout: seconds, 120 by default, 600 at most. " +
		"background: returns at once, for servers, watchers and long builds; you are told when it ends. " +
		"until (a regexp) and wait (seconds, 30 by default with until, 600 at most): with background, return when a line matches, the time is up or it ends, with what it printed so far, as a server's start. " +
		"Every command is a job, [job N] after its output; a long output comes cut to its start and end, and is kept whole."},
	{"output", "id: the output and state of a job; without id, the jobs of this conversation; without id but with match, the matching lines of every job, as job:line: text."},
	{"kill", "id: stop a job and what it started."},
}

func bashTool() Tool {
	return Tool{
		Name:        "bash",
		Description: "Run a shell command. Each is a job whose whole output stays: output reads it, kill stops it, help explains.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"command":{"type":"string"},"action":{"type":"string","enum":["run","output","kill","help"]},
"timeout":{"type":"integer"},"background":{"type":"boolean"},"until":{"type":"string"},"wait":{"type":"integer"},"id":{"type":"integer"},"topic":{"type":"string"}}}`),
		Run:      runBash,
		View:     true,
		ViewSelf: true,
		Summary: func(args json.RawMessage) string {
			if command := argField(args, "command"); command != "" {
				return command
			}
			var fields struct {
				ID int `json:"id"`
			}
			_ = json.Unmarshal(args, &fields)
			summary := BashAction(args)
			if fields.ID != 0 {
				summary += fmt.Sprintf(" job %d", fields.ID)
			}
			return strings.TrimSpace(summary + " " + argField(args, "topic") + " " + argField(args, "match"))
		},
		Changes:          BashChanges,
		AlreadyConfirmed: func(args json.RawMessage) bool { return !BashChanges(args) },
	}
}

// BashChanges tells whether a call runs or stops something; reading output
// and help do neither, and never ask.
func BashChanges(args json.RawMessage) bool {
	action := BashAction(args)
	return action == "run" || action == "kill"
}

// BashAction is what a call does: run when it has a command and says
// nothing else, output when it names a job.
func BashAction(args json.RawMessage) string {
	if action := argField(args, "action"); action != "" {
		return action
	}
	var fields struct {
		ID int `json:"id"`
	}
	_ = json.Unmarshal(args, &fields)
	if argField(args, "command") == "" && fields.ID != 0 {
		return "output"
	}
	return "run"
}

func runBash(ctx context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Command    string `json:"command"`
		Timeout    int    `json:"timeout"`
		Background bool   `json:"background"`
		Until      string `json:"until"`
		Wait       int    `json:"wait"`
		ID         int    `json:"id"`
		Topic      string `json:"topic"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	view, given := ViewFrom(ctx)
	switch action := BashAction(raw); action {
	case "help":
		text, err := help(bashHelp, args.Topic)
		return view.Apply(text), err
	case "output":
		return bashOutput(env, args.ID, view)
	case "kill":
		if env.Jobs == nil {
			return "", errors.New(jobsUnavailable)
		}
		if err := env.Jobs.Kill(args.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("job %d killed", args.ID), nil
	case "run":
	default:
		return "", fmt.Errorf("action %q: use run, output, kill or help", action)
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", errors.New("command is empty")
	}
	if args.Background {
		if env.Jobs == nil {
			return "", errors.New(jobsUnavailable)
		}
		var until *regexp.Regexp
		if args.Until != "" {
			re, err := regexp.Compile(args.Until)
			if err != nil {
				return "", fmt.Errorf("until: %w", err)
			}
			until = re
		}
		status, err := env.Jobs.Start(env, args.Command)
		if err != nil {
			return "", err
		}
		if until == nil && args.Wait <= 0 {
			return fmt.Sprintf(jobStartedResult, status.ID, status.ID), nil
		}
		wait := bashDefaultWait
		if args.Wait > 0 {
			wait = min(time.Duration(args.Wait)*time.Second, bashMaxTimeout)
		}
		return waitForStart(ctx, env.Jobs, status.ID, until, wait)
	}
	timeout := bashDefaultTimeout
	if args.Timeout > 0 {
		timeout = min(time.Duration(args.Timeout)*time.Second, bashMaxTimeout)
	}
	if env.Jobs.ready() {
		return runAsJob(ctx, env, args.Command, timeout, view, given)
	}
	output, err := runAlone(ctx, env, args.Command, timeout)
	if given {
		output = view.Apply(output)
	}
	return output, err
}

// waitForStart watches a job just started in the background until a line
// of its output matches until, wait passes or it ends, and says which with
// what it printed so far. One that ended is not told again later.
func waitForStart(ctx context.Context, jobs *Jobs, id int, until *regexp.Regexp, wait time.Duration) (string, error) {
	started := time.Now()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(bashWaitPoll)
	defer tick.Stop()
	for {
		status, printed := jobs.Printed(id)
		matched := until != nil && slices.ContainsFunc(strings.Split(printed, "\n"), until.MatchString)
		var how string
		switch {
		case matched:
			how = fmt.Sprintf("a line matched %q after %s; it goes on in the background", until.String(), time.Since(started).Round(100*time.Millisecond))
		case status.State != JobRunning:
			jobs.Told(id)
			how = "it ended: " + status.Describe()
		}
		if how == "" {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-deadline.C:
				how = fmt.Sprintf("still running after %s", wait)
				if until != nil {
					how += fmt.Sprintf(", no line matched %q yet", until.String())
				}
			case <-tick.C:
				continue
			}
		}
		return fmt.Sprintf("Job %d in the background: %s. You are told when it ends.\nOutput so far:\n%s\n[job %d]", id, how, orNoOutput(strings.TrimSuffix(printed, "\n")), id), nil
	}
}

// runAlone runs a command where no job can keep it, as when mo answers
// once, keeping the start and end of what it prints.
func runAlone(ctx context.Context, env Env, command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
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
		return output.String() + "\n" + bashUncaptured, nil
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

// runAsJob runs a command as a job of the conversation, so its whole output
// stays to read. What comes back is the view asked for, or else all of the
// output when it fits, its start and end when not.
func runAsJob(ctx context.Context, env Env, command string, timeout time.Duration, view View, given bool) (string, error) {
	status, summary, err := env.Jobs.Run(ctx, env, command, timeout)
	if status.ID == 0 {
		return "", err
	}
	if status.State == JobRunning {
		return fmt.Sprintf("The user sent this command to the background after %s; it goes on as job %d and you are told when it ends. Output so far:\n%s\n[job %d]",
			time.Since(status.Started).Round(time.Second), status.ID, orNoOutput(strings.TrimSuffix(summary, "\n")), status.ID), err
	}
	if given {
		_, text, textErr := env.Jobs.Text(status.ID)
		if textErr != nil {
			text = summary
		}
		return view.Apply(text) + fmt.Sprintf("[job %d]", status.ID), err
	}
	whole := status.Bytes <= int64(len(summary))
	summary = orNoOutput(strings.TrimSuffix(summary, "\n"))
	footer := fmt.Sprintf("[job %d]", status.ID)
	if !whole || (env.MaxOutput > 0 && len(summary)+len(footer)+1 > env.MaxOutput) {
		footer = fmt.Sprintf("[job %d: %s of output, cut; action output with id %d shows any part]", status.ID, byteSize(status.Bytes), status.ID)
		if !env.Jobs.Kept() {
			footer = fmt.Sprintf("[job %d: %s of output, cut; the rest was not kept]", status.ID, byteSize(status.Bytes))
		}
		if env.MaxOutput > 0 {
			summary = Truncate(summary, max(env.MaxOutput-len(footer)-len(cutMarker)-24, 0))
		}
	}
	return summary + "\n" + footer, err
}

// bashOutput shows a job's output and state, the list of jobs, or the
// lines of every job that match.
func bashOutput(env Env, id int, view View) (string, error) {
	if env.Jobs == nil {
		return "", errors.New(jobsUnavailable)
	}
	if id != 0 {
		status, text, err := env.Jobs.Text(id)
		if err != nil {
			return "", err
		}
		return status.Describe() + "\n" + orNoOutput(view.Apply(text)), nil
	}
	if view.re == nil {
		return view.Apply(listJobs(env.Jobs.All())), nil
	}
	found := searchJobs(env.Jobs, view.re)
	if found == "" {
		return fmt.Sprintf("no job printed a line matching %q", view.Match), nil
	}
	view.re, view.Numbered = nil, false
	return view.Apply(found), nil
}

func orNoOutput(text string) string {
	if strings.TrimSpace(text) == "" {
		return "(no output)"
	}
	return text
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
