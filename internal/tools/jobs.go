package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	jobKeptOutput    = 1 << 20
	jobShownLines    = 40
	jobEndedTail     = 15
	jobStopWait      = 3 * time.Second
	JobRunning       = "running"
	JobDone          = "done"
	JobFailed        = "failed"
	JobKilled        = "killed"
	jobsToolName     = "jobs"
	jobsUnavailable  = "background jobs are not available here: run the command without background"
	jobNotFound      = "no job %d; jobs list names them"
	jobStartedResult = "started job %d in the background. You are told when it ends; jobs output %d reads its output so far."
)

// Jobs runs commands in the background for the whole session and tells
// whoever listens when one ends. A job outlives the turn that started it,
// and mo kills what is left when it exits.
type Jobs struct {
	mu     sync.Mutex
	next   int
	jobs   []*job
	ended  []JobStatus
	onEnd  func(JobStatus)
	closed bool
}

type job struct {
	status JobStatus
	output *HeadTailBuffer
	cmd    *exec.Cmd
	done   chan struct{}
}

// JobStatus is what is known of a job.
type JobStatus struct {
	ID       int
	Command  string
	State    string
	ExitCode int
	Started  time.Time
	Ended    time.Time
}

func NewJobs() *Jobs {
	return &Jobs{}
}

// OnEnd sets what is told when a job ends; it runs on the job's goroutine.
func (j *Jobs) OnEnd(notify func(JobStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.onEnd = notify
}

// Start runs a command in its own session in the working directory.
func (j *Jobs) Start(env Env, command string) (JobStatus, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return JobStatus{}, errors.New("mo is exiting")
	}
	j.next++
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = env.WorkDir
	output := NewHeadTailBuffer(jobKeptOutput)
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return JobStatus{}, err
	}
	running := &job{
		status: JobStatus{ID: j.next, Command: command, State: JobRunning, Started: time.Now()},
		output: output, cmd: cmd, done: make(chan struct{}),
	}
	j.jobs = append(j.jobs, running)
	go j.wait(running)
	return running.status, nil
}

func (j *Jobs) wait(running *job) {
	err := running.cmd.Wait()
	j.mu.Lock()
	status := &running.status
	status.Ended = time.Now()
	var exitErr *exec.ExitError
	switch {
	case status.State == JobKilled:
	case err == nil:
		status.State = JobDone
	case errors.As(err, &exitErr):
		status.State, status.ExitCode = JobFailed, exitErr.ExitCode()
	default:
		status.State, status.ExitCode = JobFailed, -1
	}
	ended := *status
	if ended.State != JobKilled {
		j.ended = append(j.ended, ended)
	}
	notify := j.onEnd
	close(running.done)
	j.mu.Unlock()
	if notify != nil {
		notify(ended)
	}
}

// List returns every job of the session, the latest first.
func (j *Jobs) List() []JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	var statuses []JobStatus
	for _, each := range slices.Backward(j.jobs) {
		statuses = append(statuses, each.status)
	}
	return statuses
}

// TakeEnded returns the jobs that ended since it was last called, other
// than those killed on purpose, to tell the model.
func (j *Jobs) TakeEnded() []JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	ended := j.ended
	j.ended = nil
	return ended
}

// HasEnded tells whether some job ended that the model was not told about.
func (j *Jobs) HasEnded() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.ended) > 0
}

// Output returns the last lines a job wrote.
func (j *Jobs) Output(id, lines int) (JobStatus, string, error) {
	j.mu.Lock()
	found := j.find(id)
	j.mu.Unlock()
	if found == nil {
		return JobStatus{}, "", fmt.Errorf(jobNotFound, id)
	}
	j.mu.Lock()
	status := found.status
	j.mu.Unlock()
	return status, lastLines(found.output.String(), lines), nil
}

// Kill stops a job and everything it started.
func (j *Jobs) Kill(id int) error {
	j.mu.Lock()
	found := j.find(id)
	if found == nil {
		j.mu.Unlock()
		return fmt.Errorf(jobNotFound, id)
	}
	if found.status.State != JobRunning {
		j.mu.Unlock()
		return fmt.Errorf("job %d already ended", id)
	}
	found.status.State = JobKilled
	pid := found.cmd.Process.Pid
	j.mu.Unlock()
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-found.done:
	case <-time.After(jobStopWait):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-found.done
	}
	return nil
}

// Close kills every job left; nothing starts after it.
func (j *Jobs) Close() {
	j.mu.Lock()
	j.closed = true
	var running []int
	for _, each := range j.jobs {
		if each.status.State == JobRunning {
			running = append(running, each.status.ID)
		}
	}
	j.mu.Unlock()
	for _, id := range running {
		_ = j.Kill(id)
	}
}

func (j *Jobs) find(id int) *job {
	for _, each := range j.jobs {
		if each.status.ID == id {
			return each
		}
	}
	return nil
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= count {
		return strings.Join(lines, "\n")
	}
	return fmt.Sprintf("[... %d lines before ...]\n%s", len(lines)-count, strings.Join(lines[len(lines)-count:], "\n"))
}

// Describe says how a job stands, for the model and the user.
func (s JobStatus) Describe() string {
	switch s.State {
	case JobRunning:
		return fmt.Sprintf("job %d running for %s: %s", s.ID, time.Since(s.Started).Round(time.Second), s.Command)
	case JobDone:
		return fmt.Sprintf("job %d done after %s: %s", s.ID, s.Ended.Sub(s.Started).Round(time.Second), s.Command)
	case JobKilled:
		return fmt.Sprintf("job %d killed: %s", s.ID, s.Command)
	}
	return fmt.Sprintf("job %d failed with exit %d after %s: %s", s.ID, s.ExitCode, s.Ended.Sub(s.Started).Round(time.Second), s.Command)
}

// EndedNote tells the model that a job ended, with its last lines.
func (j *Jobs) EndedNote(status JobStatus) string {
	_, output, err := j.Output(status.ID, jobEndedTail)
	if err != nil || strings.TrimSpace(output) == "" {
		output = "(no output)"
	}
	return "Background " + status.Describe() + "\nLast lines:\n" + output
}

func jobsTool() Tool {
	return Tool{
		Name:        jobsToolName,
		Description: "Background jobs started with bash background=true. action: list, output (last lines of job id), or kill (job id).",
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["list","output","kill"]},"id":{"type":"integer"},"lines":{"type":"integer"}},"required":["action"]}`),
		Run: runJobs,
		Summary: func(args json.RawMessage) string {
			return strings.TrimSpace(argField(args, "action") + " " + argField(args, "id"))
		},
	}
}

func runJobs(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	if env.Jobs == nil {
		return "", errors.New(jobsUnavailable)
	}
	var args struct {
		Action string `json:"action"`
		ID     int    `json:"id"`
		Lines  int    `json:"lines"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	switch args.Action {
	case "list":
		statuses := env.Jobs.List()
		if len(statuses) == 0 {
			return "no background jobs", nil
		}
		var lines []string
		for _, status := range statuses {
			lines = append(lines, status.Describe())
		}
		return strings.Join(lines, "\n"), nil
	case "output":
		lines := args.Lines
		if lines <= 0 {
			lines = jobShownLines
		}
		status, output, err := env.Jobs.Output(args.ID, lines)
		if err != nil {
			return "", err
		}
		return status.Describe() + "\n" + output, nil
	case "kill":
		if err := env.Jobs.Kill(args.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("job %d killed", args.ID), nil
	}
	return "", fmt.Errorf("action %q: use list, output or kill", args.Action)
}
