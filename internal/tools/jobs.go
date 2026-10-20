package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jg/mo/internal/proctree"
	"github.com/jg/mo/internal/sessions"
)

const (
	jobShownLines    = 40
	jobEndedTail     = 15
	jobStopWait      = 3 * time.Second
	jobSummaryBytes  = 64 << 10
	jobFooterRoom    = 160
	JobRunning       = "running"
	JobDone          = "done"
	JobFailed        = "failed"
	JobKilled        = "killed"
	JobLost          = "lost"
	jobsUnavailable  = "background jobs are not available here: run the command without background"
	jobNotFound      = "no job %d; bash action output without id lists them"
	jobPrunedNote    = "the output of job %d was deleted: older than the turns kept, or past the 500 MB a conversation keeps"
	jobStartedResult = "started job %d in the background. You are told when it ends; action output with id %d reads its output, also while it runs."
)

// Where the output of commands is kept: in the conversation's folder, in
// memory until mo exits, or not at all.
const (
	OutputsDisk   = "disk"
	OutputsMemory = "memory"
	OutputsNone   = "none"
)

// jobsKeptBytes is how much output a conversation keeps; past it the
// oldest goes.
var jobsKeptBytes int64 = 500 << 20

// Jobs runs the commands of bash, in the foreground or in the background,
// and keeps the output of each in the folder of the conversation, so the
// model can read any part of it later, also after mo restarts. A job is
// numbered within its conversation; one in the background outlives the
// turn that started it, and mo kills what is left when it exits.
type Jobs struct {
	mu           sync.Mutex
	root         string
	conversation string
	jobs         []*job
	ended        []JobStatus
	onEnd        func(JobStatus)
	onStart      func(JobStatus)
	closed       bool
	// outputs is where output is kept, keepTurns for how many of the
	// user's turns (0 for good), and turn the one in progress.
	outputs   string
	keepTurns int
	turn      int
}

type job struct {
	status  JobStatus
	dir     string
	onDisk  bool
	log     outputLog
	summary *HeadTailBuffer
	cmd     *exec.Cmd
	done    chan struct{}
	// detached closes when a foreground command goes to the background.
	detached chan struct{}
	// told is set when the model saw the job end without a note.
	told bool
}

// JobStatus is what is known of a job. Conversation is the one it ran in.
type JobStatus struct {
	ID           int       `json:"id"`
	Command      string    `json:"command"`
	State        string    `json:"state"`
	ExitCode     int       `json:"exit_code"`
	Started      time.Time `json:"started"`
	Ended        time.Time `json:"ended,omitzero"`
	Foreground   bool      `json:"foreground,omitempty"`
	Bytes        int64     `json:"bytes"`
	Pruned       bool      `json:"pruned,omitempty"`
	Turn         int       `json:"turn,omitempty"`
	Conversation string    `json:"-"`
}

// NewJobs keeps the output of commands under root, the folder of the saved
// conversations.
func NewJobs(root string) *Jobs {
	return &Jobs{root: root, outputs: OutputsDisk}
}

// Configure sets where output is kept from now on, and for how many turns.
func (j *Jobs) Configure(outputs string, keepTurns int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if outputs != OutputsMemory && outputs != OutputsNone {
		outputs = OutputsDisk
	}
	j.outputs, j.keepTurns = outputs, keepTurns
	j.pruneTurns()
}

// SetTurn names the user's turn in progress, counted in the conversation,
// and lets go of the output of turns past those kept.
func (j *Jobs) SetTurn(turn int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.turn = turn
	j.pruneTurns()
}

// Kept tells whether output is kept to read later.
func (j *Jobs) Kept() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.outputs != OutputsNone
}

// pruneTurns lets go of the output of the jobs of turns older than those
// kept, in memory and on disk; the lock is held.
func (j *Jobs) pruneTurns() {
	if j.keepTurns <= 0 || j.conversation == "" || j.turn == 0 {
		return
	}
	oldest := j.turn - j.keepTurns
	for _, each := range j.jobs {
		status := &each.status
		if status.Conversation != j.conversation || status.State == JobRunning || status.Pruned || status.Turn > oldest {
			continue
		}
		status.Pruned = true
		each.log = &countingLog{size: each.log.Size()}
		if each.onDisk {
			os.Remove(filepath.Join(each.dir, strconv.Itoa(status.ID)+".log"))
			_ = writeJobStatus(each.dir, *status)
		}
	}
	if j.root == "" {
		return
	}
	dir := sessions.JobsDir(j.root, j.conversation)
	for _, status := range readJobStatuses(dir) {
		if status.Pruned || status.Turn > oldest || status.State == JobRunning {
			continue
		}
		status.Pruned = true
		os.Remove(filepath.Join(dir, strconv.Itoa(status.ID)+".log"))
		_ = writeJobStatus(dir, status)
	}
}

// SetConversation names the conversation in progress: the commands run from
// now on are kept in its folder, and the model sees its jobs only.
func (j *Jobs) SetConversation(id string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id != j.conversation {
		// The turn counted is the one in progress there, unknown until it
		// starts: nothing is let go of meanwhile.
		j.turn = 0
	}
	j.conversation = id
}

// OnEnd sets what is told when a background job ends; it runs on the job's
// goroutine.
func (j *Jobs) OnEnd(notify func(JobStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.onEnd = notify
}

// OnStart sets what is told when any command starts, so a panel can show
// it while it runs.
func (j *Jobs) OnStart(notify func(JobStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.onStart = notify
}

// started tells that a job started; the lock is not held.
func (j *Jobs) started(status JobStatus) {
	j.mu.Lock()
	notify := j.onStart
	j.mu.Unlock()
	if notify != nil {
		notify(status)
	}
}

// ready says whether commands can be kept: there is a conversation to keep
// them in.
func (j *Jobs) ready() bool {
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.conversation != "" && (j.root != "" || j.outputs != OutputsDisk)
}

// begin numbers a new job in the conversation and opens its log; the lock
// is held.
func (j *Jobs) begin(env Env, command string, foreground bool) (*job, error) {
	if j.closed {
		return nil, errors.New("mo is exiting")
	}
	if j.conversation == "" || (j.root == "" && j.outputs == OutputsDisk) {
		return nil, errors.New(jobsUnavailable)
	}
	created := &job{
		status:  JobStatus{Command: command, State: JobRunning, Started: time.Now(), Foreground: foreground, Conversation: j.conversation, Turn: j.turn},
		summary: NewHeadTailBuffer(summaryBytes(env.MaxOutput)), done: make(chan struct{}), detached: make(chan struct{}),
	}
	switch j.outputs {
	case OutputsMemory:
		created.log = newMemoryLog()
	case OutputsNone:
		created.log = &countingLog{}
	}
	if j.root != "" {
		created.dir = sessions.JobsDir(j.root, j.conversation)
	}
	created.status.ID = j.nextID(created.dir)
	if created.log == nil {
		if err := os.MkdirAll(created.dir, 0o700); err != nil {
			return nil, err
		}
		j.prune(created.dir)
		log, err := newJobLog(filepath.Join(created.dir, strconv.Itoa(created.status.ID)+".log"))
		if err != nil {
			return nil, err
		}
		created.log, created.onDisk = log, true
		_ = writeJobStatus(created.dir, created.status)
	}
	j.jobs = append(j.jobs, created)
	return created, nil
}

// summaryBytes is how much of a command's output comes back with it: what
// the model is sent, less room for the line saying where the rest is.
func summaryBytes(maxOutput int) int {
	if maxOutput <= 0 {
		return jobSummaryBytes
	}
	return max(maxOutput-jobFooterRoom, jobFooterRoom)
}

// nextID follows the highest number used in the folder or in this run.
func (j *Jobs) nextID(dir string) int {
	highest := 0
	var entries []os.DirEntry
	if dir != "" {
		entries, _ = os.ReadDir(dir)
	}
	for _, entry := range entries {
		if number, isStatus := strings.CutSuffix(entry.Name(), ".json"); isStatus {
			if id, err := strconv.Atoi(number); err == nil {
				highest = max(highest, id)
			}
		}
	}
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation {
			highest = max(highest, each.status.ID)
		}
	}
	return highest + 1
}

// prune deletes the output of the oldest ended jobs while the folder holds
// more than jobsKeptBytes; the lock is held.
func (j *Jobs) prune(dir string) {
	statuses := readJobStatuses(dir)
	var total int64
	sizes := map[int]int64{}
	for _, status := range statuses {
		if info, err := os.Stat(filepath.Join(dir, strconv.Itoa(status.ID)+".log")); err == nil {
			sizes[status.ID] = info.Size()
			total += info.Size()
		}
	}
	for _, status := range statuses {
		if total <= jobsKeptBytes {
			return
		}
		if sizes[status.ID] == 0 || j.live(dir, status.ID) != nil {
			continue
		}
		if os.Remove(filepath.Join(dir, strconv.Itoa(status.ID)+".log")) == nil {
			total -= sizes[status.ID]
			status.Pruned = true
			_ = writeJobStatus(dir, status)
			for _, each := range j.jobs {
				if each.dir == dir && each.status.ID == status.ID {
					each.status.Pruned = true
				}
			}
		}
	}
}

// live is the job of this run of the conversation in progress with that
// number that still runs.
func (j *Jobs) live(_ string, id int) *job {
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation && each.status.ID == id && each.status.State == JobRunning {
			return each
		}
	}
	return nil
}

// Start runs a command in the background, in its own session, in the
// working directory.
func (j *Jobs) Start(env Env, command string) (JobStatus, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	started, err := j.begin(env, command, false)
	if err != nil {
		return JobStatus{}, err
	}
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = env.WorkDir
	output := io.MultiWriter(started.log, started.summary)
	cmd.Stdout, cmd.Stderr = output, output
	proctree.Detach(cmd)
	if err := cmd.Start(); err != nil {
		j.forget(started)
		return JobStatus{}, err
	}
	started.cmd = cmd
	go j.wait(started)
	go j.started(started.status)
	return started.status, nil
}

// forget drops a job that never started, its files too; the lock is held.
func (j *Jobs) forget(failed *job) {
	failed.log.Close()
	j.jobs = slices.DeleteFunc(j.jobs, func(each *job) bool { return each == failed })
	if !failed.onDisk {
		return
	}
	base := filepath.Join(failed.dir, strconv.Itoa(failed.status.ID))
	os.Remove(base + ".log")
	os.Remove(base + ".json")
}

// Run runs a command in the foreground until it ends, timeout passes, ctx
// is cancelled or the user sends it to the background, and returns its
// status and the start and end of its output so far. A command sent to the
// background goes on as a job, told to the model when it ends.
func (j *Jobs) Run(ctx context.Context, env Env, command string, timeout time.Duration) (JobStatus, string, error) {
	j.mu.Lock()
	running, err := j.begin(env, command, true)
	if err != nil {
		j.mu.Unlock()
		return JobStatus{}, "", err
	}
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = env.WorkDir
	output := io.MultiWriter(running.log, running.summary)
	cmd.Stdout, cmd.Stderr = output, output
	proctree.Detach(cmd)
	cmd.WaitDelay = bashWaitForOutput
	if err := cmd.Start(); err != nil {
		j.forget(running)
		j.mu.Unlock()
		return JobStatus{}, "", err
	}
	running.cmd = cmd
	status := running.status
	j.mu.Unlock()
	go j.wait(running)
	j.started(status)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-running.done:
	case <-running.detached:
		j.mu.Lock()
		status := j.current(running)
		j.mu.Unlock()
		return status, running.summary.String(), nil
	case <-timer.C:
		j.stop(running)
		return running.status, running.summary.String(), fmt.Errorf("timed out after %s", timeout)
	case <-ctx.Done():
		j.stop(running)
		return running.status, running.summary.String(), ctx.Err()
	}
	j.mu.Lock()
	status = running.status
	j.mu.Unlock()
	if status.State == JobKilled {
		return status, running.summary.String(), errors.New("killed by the user")
	}
	if status.State == JobFailed {
		return status, running.summary.String(), fmt.Errorf("exit status %d", status.ExitCode)
	}
	return status, running.summary.String(), nil
}

// Detach sends the commands running in the foreground of the conversation
// in progress to the background, as the user asks when one blocks the
// model.
func (j *Jobs) Detach() []JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	var moved []JobStatus
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation && each.status.Foreground && each.status.State == JobRunning {
			moved = append(moved, j.detach(each))
		}
	}
	return moved
}

// DetachJob sends one command of the conversation in progress to the
// background.
func (j *Jobs) DetachJob(id int) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.conversation == "" {
		return fmt.Errorf(jobNotFound, id)
	}
	running := j.live(sessions.JobsDir(j.root, j.conversation), id)
	switch {
	case running == nil:
		return fmt.Errorf("job %d is not running", id)
	case !running.status.Foreground:
		return fmt.Errorf("job %d runs in the background already", id)
	}
	j.detach(running)
	return nil
}

// detach lets the model go on without a job; the lock is held.
func (j *Jobs) detach(running *job) JobStatus {
	running.status.Foreground = false
	if running.onDisk {
		_ = writeJobStatus(running.dir, running.status)
	}
	close(running.detached)
	return j.current(running)
}

// RunningCommand is the job of the conversation in progress that runs
// command in the foreground, the latest if more do.
func (j *Jobs) RunningCommand(command string) (int, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, each := range slices.Backward(j.jobs) {
		status := each.status
		if status.Conversation == j.conversation && status.Foreground && status.State == JobRunning && status.Command == command {
			return status.ID, true
		}
	}
	return 0, false
}

func (j *Jobs) wait(running *job) {
	_ = running.cmd.Wait()
	status := j.finish(running, running.cmd.ProcessState)
	if status.Foreground {
		return
	}
	j.mu.Lock()
	if status.State != JobKilled && !running.told {
		j.ended = append(j.ended, status)
	}
	notify := j.onEnd
	j.mu.Unlock()
	if notify != nil {
		notify(status)
	}
}

// finish closes a job's log and records how it ended.
func (j *Jobs) finish(running *job, state *os.ProcessState) JobStatus {
	_ = running.log.Close()
	j.mu.Lock()
	defer j.mu.Unlock()
	status := &running.status
	status.Ended = time.Now()
	status.Bytes = running.log.Size()
	switch {
	case status.State == JobKilled:
	case state != nil && state.Success():
		status.State = JobDone
	case state != nil && state.ExitCode() >= 0:
		status.State, status.ExitCode = JobFailed, state.ExitCode()
	default:
		status.State, status.ExitCode = JobFailed, -1
	}
	if running.onDisk {
		_ = writeJobStatus(running.dir, *status)
	}
	close(running.done)
	return *status
}

// List returns the jobs of the conversation in progress run since mo
// started, the latest first: those in the background, and those in the
// foreground while they run.
func (j *Jobs) List() []JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	var statuses []JobStatus
	for _, each := range slices.Backward(j.jobs) {
		if each.status.Conversation == j.conversation && (!each.status.Foreground || each.status.State == JobRunning) {
			statuses = append(statuses, j.current(each))
		}
	}
	return statuses
}

// current is a job's status with the output written so far; the lock is held.
func (j *Jobs) current(each *job) JobStatus {
	status := each.status
	if status.State == JobRunning {
		status.Bytes = each.log.Size()
	}
	return status
}

// All returns every job of the conversation in progress, those of earlier
// runs of mo kept on disk too, the latest first.
func (j *Jobs) All() []JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.conversation == "" {
		return nil
	}
	var statuses []JobStatus
	if j.root != "" {
		dir := sessions.JobsDir(j.root, j.conversation)
		for _, status := range readJobStatuses(dir) {
			statuses = append(statuses, j.known(dir, status))
		}
	}
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation && !each.onDisk {
			statuses = append(statuses, j.current(each))
		}
	}
	slices.SortFunc(statuses, func(a, b JobStatus) int { return b.ID - a.ID })
	return statuses
}

// known is a status read from disk as this run knows it: current while it
// runs here, lost when it was left running by a mo that is gone; the lock
// is held.
func (j *Jobs) known(dir string, status JobStatus) JobStatus {
	status.Conversation = j.conversation
	if status.State != JobRunning {
		return status
	}
	if running := j.live(dir, status.ID); running != nil {
		return j.current(running)
	}
	for _, each := range j.jobs {
		if each.status.State == JobRunning && each.status.Command == status.Command && each.status.Started.Equal(status.Started) {
			return j.current(each)
		}
	}
	status.State = JobLost
	return status
}

// Status is how a job of the conversation in progress stands.
func (j *Jobs) Status(id int) (JobStatus, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	status, _, err := j.lookup(id)
	return status, err
}

// lookup finds a job of the conversation in progress, and its output if
// this run has it; the lock is held.
func (j *Jobs) lookup(id int) (JobStatus, outputLog, error) {
	if j.conversation == "" {
		return JobStatus{}, nil, fmt.Errorf(jobNotFound, id)
	}
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation && each.status.ID == id {
			return j.current(each), each.log, nil
		}
	}
	if j.root == "" {
		return JobStatus{}, nil, fmt.Errorf(jobNotFound, id)
	}
	dir := sessions.JobsDir(j.root, j.conversation)
	status, err := readJobStatus(dir, id)
	if err != nil {
		return JobStatus{}, nil, fmt.Errorf(jobNotFound, id)
	}
	return j.known(dir, status), nil, nil
}

// Text is all the output a job of the conversation in progress kept.
func (j *Jobs) Text(id int) (JobStatus, string, error) {
	j.mu.Lock()
	status, log, err := j.lookup(id)
	dir := ""
	if j.root != "" {
		dir = sessions.JobsDir(j.root, j.conversation)
	}
	j.mu.Unlock()
	if err != nil {
		return status, "", err
	}
	if status.Pruned {
		return status, "", fmt.Errorf(jobPrunedNote, id)
	}
	if log != nil {
		text, err := log.Text()
		return status, text, err
	}
	text, err := readJobLog(filepath.Join(dir, strconv.Itoa(id)+".log"), true, 0)
	return status, text, err
}

// Output returns the last lines a job wrote.
func (j *Jobs) Output(id, lines int) (JobStatus, string, error) {
	status, text, err := j.Text(id)
	if err != nil {
		return status, "", err
	}
	return status, lastLines(text, lines), nil
}

// Kill stops a job of the conversation in progress and everything it
// started.
func (j *Jobs) Kill(id int) error {
	j.mu.Lock()
	var found *job
	if j.conversation != "" {
		found = j.live(sessions.JobsDir(j.root, j.conversation), id)
	}
	j.mu.Unlock()
	if found == nil {
		if _, err := j.Status(id); err != nil {
			return err
		}
		return fmt.Errorf("job %d is not running", id)
	}
	j.stop(found)
	return nil
}

// stop kills a running job and what it started, gently first.
func (j *Jobs) stop(running *job) {
	j.mu.Lock()
	if running.status.State != JobRunning || running.cmd == nil || running.cmd.Process == nil {
		j.mu.Unlock()
		return
	}
	running.status.State = JobKilled
	pid := running.cmd.Process.Pid
	j.mu.Unlock()
	_ = proctree.Terminate(pid)
	select {
	case <-running.done:
	case <-time.After(jobStopWait):
		_ = proctree.Kill(pid)
		<-running.done
	}
}

// KillConversation stops the jobs that run in a conversation, before it is
// deleted.
func (j *Jobs) KillConversation(id string) {
	j.mu.Lock()
	var running []*job
	for _, each := range j.jobs {
		if each.status.Conversation == id && each.status.State == JobRunning {
			running = append(running, each)
		}
	}
	j.mu.Unlock()
	for _, each := range running {
		j.stop(each)
	}
}

// Close kills every job left; nothing starts after it.
func (j *Jobs) Close() {
	j.mu.Lock()
	j.closed = true
	var running []*job
	for _, each := range j.jobs {
		if each.status.State == JobRunning {
			running = append(running, each)
		}
	}
	j.mu.Unlock()
	for _, each := range running {
		j.stop(each)
	}
}

// TakeEnded returns the background jobs of the conversation in progress
// that ended since it was last called, other than those killed on purpose,
// to tell the model. Those of other conversations wait until it is theirs.
func (j *Jobs) TakeEnded() []JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	var taken, kept []JobStatus
	for _, status := range j.ended {
		if status.Conversation == j.conversation {
			taken = append(taken, status)
		} else {
			kept = append(kept, status)
		}
	}
	j.ended = kept
	return taken
}

// Printed is how a job of the conversation in progress stands and the
// start and end of what it printed so far, as the model gets a command's.
func (j *Jobs) Printed(id int) (JobStatus, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation && each.status.ID == id {
			return j.current(each), each.summary.String()
		}
	}
	return JobStatus{}, ""
}

// Told takes a job out of those the model is to hear ended: it saw so
// already.
func (j *Jobs) Told(id int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, each := range j.jobs {
		if each.status.Conversation == j.conversation && each.status.ID == id {
			each.told = true
		}
	}
	j.ended = slices.DeleteFunc(j.ended, func(status JobStatus) bool {
		return status.Conversation == j.conversation && status.ID == id
	})
}

// HasEnded tells whether a job of the conversation in progress ended that
// the model was not told about.
func (j *Jobs) HasEnded() bool {
	return len(j.Untold()) > 0
}

// Untold names the jobs of the conversation in progress that ended without
// the model being told yet.
func (j *Jobs) Untold() map[int]bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	untold := map[int]bool{}
	for _, status := range j.ended {
		if status.Conversation == j.conversation {
			untold[status.ID] = true
		}
	}
	return untold
}

func writeJobStatus(dir string, status JobStatus) error {
	encoded, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, strconv.Itoa(status.ID)+".json"), encoded, 0o600)
}

func readJobStatus(dir string, id int) (JobStatus, error) {
	var status JobStatus
	data, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(id)+".json"))
	if err != nil {
		return status, err
	}
	err = json.Unmarshal(data, &status)
	return status, err
}

// readJobStatuses reads every job of a folder, the oldest first.
func readJobStatuses(dir string) []JobStatus {
	entries, _ := os.ReadDir(dir)
	var statuses []JobStatus
	for _, entry := range entries {
		number, isStatus := strings.CutSuffix(entry.Name(), ".json")
		id, err := strconv.Atoi(number)
		if !isStatus || err != nil {
			continue
		}
		if status, err := readJobStatus(dir, id); err == nil {
			statuses = append(statuses, status)
		}
	}
	slices.SortFunc(statuses, func(a, b JobStatus) int { return a.ID - b.ID })
	return statuses
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
	case JobLost:
		return fmt.Sprintf("job %d stopped when mo exited: %s", s.ID, s.Command)
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

// listJobs is one line for each job, the latest first.
func listJobs(statuses []JobStatus) string {
	if len(statuses) == 0 {
		return "no commands run in this conversation yet"
	}
	var lines []string
	for _, status := range statuses {
		line := status.Describe()
		if !status.Foreground {
			line += " · background"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// searchJobs gives the lines of every job of the conversation that match,
// as job:line: text, oldest job first.
func searchJobs(jobs *Jobs, re *regexp.Regexp) string {
	var out strings.Builder
	for _, status := range slices.Backward(jobs.All()) {
		_, text, err := jobs.Text(status.ID)
		if err != nil {
			continue
		}
		for number, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
			if re.MatchString(line) {
				fmt.Fprintf(&out, "%d:%d: %s\n", status.ID, number+1, line)
			}
		}
	}
	return out.String()
}

func byteSize(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", size)
}
