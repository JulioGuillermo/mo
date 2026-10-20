package tui

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | SB-01 | Happy    | A wide terminal shows tasks and jobs beside the conversation, narrower | P0     |
// | SB-02 | Happy    | A narrow terminal, /sidebar or the setting off keep the tasks above the input | P1 |
// | SB-03 | Happy    | A job ending while idle starts a turn; while working it is only noted | P0       |
// | SB-04 | Happy    | After a stop, /jobs tell lets the model hear the jobs that ended      | P1       |
// | SB-06 | Happy    | on shows the panel in a narrow terminal while the conversation keeps its minimum; /sidebar alone turns it on and off | P0 |
// | SB-07 | Edge     | /sidebar takes auto, on or off and suggests them; anything else only gets a note | P1 |
// | SB-05 | Happy    | A job that ended in a turn stopped with esc still starts a turn, with a note | P0 |

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/tools"
)

func sidebarModel(t *testing.T, width int) model {
	t.Helper()
	m := conversationModel()
	jobs := tools.NewJobs(t.TempDir())
	jobs.SetConversation("c1")
	t.Cleanup(jobs.Close)
	m.session.Agent = &agent.Agent{Tools: tools.NewRegistry(tools.Env{WorkDir: t.TempDir(), Jobs: jobs}, tools.Options{})}
	m.width, m.height = width, 30
	m.printer.setWidth(m.mainWidth())
	m.todos = []tools.Todo{{Text: "read the code", Status: "completed"}, {Text: "write the sidebar with a long task name that wraps", Status: "in_progress"}}
	_, _ = jobs.Start(tools.Env{WorkDir: t.TempDir()}, "sleep 30")
	return m
}

func TestSidebar(t *testing.T) {
	t.Run("SB-01 a wide terminal shows tasks and jobs beside the conversation, narrower", func(t *testing.T) {
		m := sidebarModel(t, 140)

		view := steady(ansi.Strip(m.View().Content))

		for _, want := range []string{"Tasks  1/2", "✓ read the code", "◐ write the sidebar", "that wraps", "Jobs", "◜ 1 sleep 30", "running"} {
			if !strings.Contains(view, want) {
				t.Errorf("missing %q", want)
			}
		}
		if m.mainWidth() != 140-sidebarWidth-1 || strings.Contains(ansi.Strip(m.bottom()), "todo 1/2") {
			t.Errorf("main width = %d", m.mainWidth())
		}
	})

	t.Run("SB-02 a narrow terminal, /sidebar or the setting off keep the tasks above the input", func(t *testing.T) {
		narrow := sidebarModel(t, 90)
		toggled, _ := sidebarModel(t, 140).runCommand("/sidebar")
		off := sidebarModel(t, 140)
		off.session.Config.Render.Sidebar = sidebarOff

		for name, m := range map[string]model{"narrow": narrow, "toggled": toggled.(model), "off": off} {
			if m.sidebarShown() || !strings.Contains(ansi.Strip(m.bottom()), "todo 1/2") || m.mainWidth() != m.width {
				t.Errorf("%s: sidebar shown = %v", name, m.sidebarShown())
			}
		}
	})

	t.Run("SB-06 on shows the panel in a narrow terminal while the conversation keeps its minimum; /sidebar alone turns it on and off", func(t *testing.T) {
		vertical := sidebarModel(t, 80)
		vertical.session.Config.Render.Sidebar = sidebarOn
		tooNarrow := sidebarModel(t, sidebarWidth+mainMinWidth)
		tooNarrow.session.Config.Render.Sidebar = sidebarOn
		turnedOn, _ := sidebarModel(t, 90).runCommand("/sidebar")
		turnedOff, _ := turnedOn.(model).runCommand("/sidebar")

		if !vertical.sidebarShown() || vertical.mainWidth() != 80-sidebarWidth-1 || !strings.Contains(steady(ansi.Strip(vertical.View().Content)), "Tasks  1/2") {
			t.Errorf("on in 80 columns: shown = %v main = %d", vertical.sidebarShown(), vertical.mainWidth())
		}
		if tooNarrow.sidebarShown() {
			t.Error("on left the conversation narrower than its minimum")
		}
		if !turnedOn.(model).sidebarShown() || turnedOff.(model).sidebarShown() || turnedOff.(model).sidebarSession != sidebarOff {
			t.Errorf("/sidebar: on = %v off = %v", turnedOn.(model).sidebarShown(), turnedOff.(model).sidebarShown())
		}
	})

	t.Run("SB-07 /sidebar takes auto, on or off and suggests them; anything else only gets a note", func(t *testing.T) {
		m := sidebarModel(t, 140)
		auto, _ := m.runCommand("/sidebar auto")
		wrong, _ := m.runCommand("/sidebar maybe")
		m.input.SetValue("/sidebar ")

		var names []string
		for _, suggestion := range m.suggestions() {
			names = append(names, suggestion.name)
		}

		if auto.(model).sidebarSession != sidebarAuto || !auto.(model).sidebarShown() || wrong.(model).sidebarSession != "" {
			t.Errorf("auto = %q wrong = %q", auto.(model).sidebarSession, wrong.(model).sidebarSession)
		}
		if strings.Join(names, ",") != "sidebar auto,sidebar on,sidebar off" {
			t.Errorf("suggestions = %v", names)
		}
	})

	t.Run("SB-03 a job ending while idle starts a turn; while working it is only noted", func(t *testing.T) {
		idle := sidebarModel(t, 140)
		jobs := idle.jobs()
		ended := make(chan tools.JobStatus, 1)
		jobs.OnEnd(func(status tools.JobStatus) { ended <- status })
		_, _ = jobs.Start(tools.Env{WorkDir: t.TempDir()}, "exit 2")
		var status tools.JobStatus
		select {
		case status = <-ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the job did not end")
		}
		working := idle
		working.running = true

		reacting, _ := idle.Update(jobEndedMsg(status))
		noted, cmd := working.Update(jobEndedMsg(status))

		if !reacting.(model).running || !strings.Contains(shown(reacting.(model)), "failed with exit 2") {
			t.Errorf("idle: running = %v", reacting.(model).running)
		}
		if cmd != nil || !strings.Contains(shown(noted.(model)), "failed with exit 2") {
			t.Error("a job ending mid-turn started something")
		}
	})
}

func TestTellJobs(t *testing.T) {
	t.Run("SB-04 after a stop, /jobs tell lets the model hear the jobs that ended; nothing new, no turn", func(t *testing.T) {
		m := sidebarModel(t, 140)
		jobs := m.jobs()
		ended := make(chan tools.JobStatus, 1)
		jobs.OnEnd(func(status tools.JobStatus) { ended <- status })
		_, _ = jobs.Start(tools.Env{WorkDir: t.TempDir()}, "exit 1")
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the job did not end")
		}
		listed, _ := m.runCommand("/jobs")
		panel := ansi.Strip(m.sidebar(30))

		told, _ := m.runCommand("/jobs tell")
		_ = jobs.TakeEnded()
		nothing, _ := m.runCommand("/jobs tell")

		if !strings.Contains(shown(listed.(model)), "the model was not told yet") || !strings.Contains(panel, "/jobs tell") {
			t.Errorf("listed:\n%s", shown(listed.(model)))
		}
		if !told.(model).running || nothing.(model).running || !strings.Contains(shown(nothing.(model)), "no job ended") {
			t.Errorf("told running = %v nothing running = %v", told.(model).running, nothing.(model).running)
		}
	})
}

func TestJobsAfterStop(t *testing.T) {
	t.Run("SB-05 a job that ended in a turn stopped with esc still starts a turn, with a note", func(t *testing.T) {
		m := sidebarModel(t, 140)
		jobs := m.jobs()
		ended := make(chan tools.JobStatus, 1)
		jobs.OnEnd(func(status tools.JobStatus) { ended <- status })
		_, _ = jobs.Start(tools.Env{WorkDir: t.TempDir()}, "exit 0")
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the job did not end")
		}
		m.running, m.cancel = true, func() {}

		stopped, cmd := m.finishRun(context.Canceled)

		if !stopped.(model).running || cmd == nil {
			t.Fatal("no turn started after esc")
		}
		var noted bool
		for _, each := range cmd().(tea.BatchMsg) {
			if each == nil {
				continue
			}
			if text, ok := each().(printMsg); ok && strings.Contains(string(text), "the model goes on to hear of it") {
				noted = true
			}
		}
		if !noted {
			t.Error("no note said why the model goes on")
		}
	})
}

func TestSidebarSections(t *testing.T) {
	t.Run("a section shows once it has something; with nothing, one line says so", func(t *testing.T) {
		m := sidebarModel(t, 140)
		withJob := ansi.Strip(m.sidebar(30))
		m.todos = nil
		onlyJob := ansi.Strip(m.sidebar(30))
		m.session.Agent = &agent.Agent{}
		empty := ansi.Strip(m.sidebar(30))

		if !strings.Contains(withJob, "Tasks") || !strings.Contains(withJob, "Jobs") {
			t.Errorf("with tasks and a job:\n%s", withJob)
		}
		if strings.Contains(onlyJob, "Tasks") || strings.Contains(onlyJob, "none yet") || !strings.Contains(onlyJob, "Jobs") {
			t.Errorf("only a job:\n%s", onlyJob)
		}
		if strings.Contains(empty, "Jobs") || strings.Contains(empty, "UIs") || !strings.Contains(empty, "no tasks, jobs or interfaces yet") {
			t.Errorf("empty:\n%s", empty)
		}
	})
}
