package tui

// Test Traceability Table
// | ID    | Category | Description                                                            | Priority |
// | SP-01 | Happy    | esc asks a stop after the calls running; esc again stops at once          | P0       |
// | SP-02 | Happy    | esc after /stop next stops sooner, after the calls running              | P1       |
// | SP-03 | Happy    | /stop cancel takes back a stop asked                                     | P1       |
// | SP-04 | Happy    | /stop now stops at once                                                  | P1       |
// | SP-05 | Happy    | The status line says a stop is coming and what esc does then             | P2       |
// | SP-06 | Edge     | /stop with nothing working says so                                      | P2       |

import (
	"strings"
	"testing"

	"github.com/jg/mo/internal/agent"
)

func workingModel(t *testing.T) (model, *bool) {
	t.Helper()
	m := conversationModel()
	stopped := false
	m.running, m.cancel = true, func() { stopped = true }
	m.session.Agent = &agent.Agent{}
	return m, &stopped
}

func stopWith(t *testing.T, m model, argument string) model {
	t.Helper()
	next, _ := m.stopCommand(argument)
	return next.(model)
}

func TestStopModes(t *testing.T) {
	t.Run("SP-01 esc asks a stop after the calls running; esc again stops at once", func(t *testing.T) {
		m, stopped := workingModel(t)

		m = keys(t, m, keyEsc)
		soft, cutAtOnce := m.stopping, *stopped
		m = keys(t, m, keyEsc)

		if soft != agent.StopAfterCalls || cutAtOnce || !m.session.Agent.Stopping() || !*stopped {
			t.Errorf("first esc %v, stopped at once %v, second esc stopped %v", soft, cutAtOnce, *stopped)
		}
	})

	t.Run("SP-02 esc after /stop next stops sooner, after the calls running", func(t *testing.T) {
		m, stopped := workingModel(t)
		m = stopWith(t, m, "next")
		next := m.stopping

		m = keys(t, m, keyEsc)

		if next != agent.StopAfterRound || m.stopping != agent.StopAfterCalls || *stopped {
			t.Errorf("next %v then esc %v, stopped %v", next, m.stopping, *stopped)
		}
	})

	t.Run("SP-03 /stop cancel takes back a stop asked", func(t *testing.T) {
		m, stopped := workingModel(t)
		m = stopWith(t, m, "")

		m = stopWith(t, m, "cancel")

		if m.stopping != agent.StopNone || m.session.Agent.Stopping() || *stopped {
			t.Errorf("stopping %v agent %v stopped %v", m.stopping, m.session.Agent.Stopping(), *stopped)
		}
	})

	t.Run("SP-04 /stop now stops at once", func(t *testing.T) {
		m, stopped := workingModel(t)

		m = stopWith(t, m, "now")

		if !*stopped || m.stopping != agent.StopNone {
			t.Errorf("stopped %v stopping %v", *stopped, m.stopping)
		}
	})

	t.Run("SP-05 the status line says a stop is coming and what esc does then", func(t *testing.T) {
		m, _ := workingModel(t)
		m = stopWith(t, m, "next")
		afterRound := m.currentState()
		m = keys(t, m, keyEsc)
		afterCalls := m.currentState()

		if afterRound.word != "stopping" || !strings.Contains(afterRound.detail, "this round") || !strings.Contains(afterCalls.detail, "esc: now") {
			t.Errorf("after round %+v, after calls %+v", afterRound, afterCalls)
		}
	})

	t.Run("SP-06 /stop with nothing working says so", func(t *testing.T) {
		m := conversationModel()

		m = stopWith(t, m, "")

		if !strings.Contains(shown(m), "nothing is working") {
			t.Errorf("shown:\n%s", shown(m))
		}
	})
}
