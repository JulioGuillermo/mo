package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PC-01 | Happy    | Live sends every piece as it comes                                        | P0       |
// | PC-02 | Happy    | By time, the pieces of an interval go together, and the last ones when the time is up | P0 |
// | PC-03 | Happy    | By tokens, every so many go together, and fewer once a second passed      | P0       |
// | PC-04 | Happy    | What is not the reply, and every other hook, sends what is held first, in order | P0  |
// | PC-05 | Happy    | A batch is drawn as its pieces would be, the speeds counting each        | P0       |
// | PC-06 | Happy    | The settings show the milliseconds or the tokens as the way chosen needs, and save them | P1 |
// | PC-07 | Happy    | Saved while the model works, the refresh applies at once, to the reply being written | P0 |

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jg/mo/internal/agent"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

// pacedSends records what a pacer sends, as the kinds of message it is.
type pacedSends struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *pacedSends) send(msg tea.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
}

func (s *pacedSends) shapes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var shapes []string
	for _, msg := range s.msgs {
		switch msg := msg.(type) {
		case streamMsg:
			shapes = append(shapes, "one:"+msg.Text)
		case streamBatchMsg:
			var texts []string
			for _, each := range msg {
				texts = append(texts, each.event.Text)
			}
			shapes = append(shapes, "batch:"+strings.Join(texts, ""))
		case string:
			shapes = append(shapes, msg)
		}
	}
	return shapes
}

func text(piece string) llm.Event {
	return llm.Event{Kind: llm.EventText, Text: piece}
}

func newPacer(pace replyPace) (*pacer, *pacedSends) {
	record := &pacedSends{}
	p := &pacer{send: record.send}
	p.setPace(pace)
	return p, record
}

func waitShapes(t *testing.T, record *pacedSends, count int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(record.shapes()) < count && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return record.shapes()
}

func TestPace(t *testing.T) {
	t.Run("PC-01 live sends every piece as it comes", func(t *testing.T) {
		p, record := newPacer(replyPace{mode: config.RefreshLive})

		p.event(text("a"))
		p.event(text("b"))

		if got := strings.Join(record.shapes(), " "); got != "one:a one:b" {
			t.Errorf("sent %s", got)
		}
	})

	t.Run("PC-02 by time, the pieces of an interval go together, and the last ones when the time is up", func(t *testing.T) {
		p, record := newPacer(replyPace{mode: config.RefreshTime, every: 60 * time.Millisecond})

		p.event(text("a"))
		p.event(text("b"))
		held := len(record.shapes())
		time.Sleep(80 * time.Millisecond)
		p.event(text("c"))
		got := waitShapes(t, record, 2)

		if held != 0 || strings.Join(got, " ") != "batch:abc" && strings.Join(got, " ") != "batch:ab batch:c" {
			t.Errorf("held %d sent %v", held, got)
		}
	})

	t.Run("PC-03 by tokens, every so many go together, and fewer once a second passed", func(t *testing.T) {
		p, record := newPacer(replyPace{mode: config.RefreshTokens, tokens: 3})

		for _, piece := range []string{"a", "b", "c", "d"} {
			p.event(text(piece))
		}
		first := strings.Join(record.shapes(), " ")
		got := waitShapes(t, record, 2)

		if first != "batch:abc" || strings.Join(got, " ") != "batch:abc batch:d" {
			t.Errorf("first %q then %v", first, got)
		}
	})

	t.Run("PC-04 what is not the reply, and every other hook, sends what is held first, in order", func(t *testing.T) {
		p, record := newPacer(replyPace{mode: config.RefreshTime, every: time.Hour})
		b := &bridge{pacer: p}
		hooks := b.flushingFirst(agent.Hooks{
			OnToolStart: func(llm.ToolCall, string) { record.send("tool start") },
			OnAssistant: func(llm.Message) { record.send("reply ended") },
		})

		p.event(text("a"))
		p.event(llm.Event{Kind: llm.EventPromptProgress, Progress: llm.PromptProgress{Done: 1, Total: 2}})
		p.event(text("b"))
		hooks.OnToolStart(llm.ToolCall{}, "")
		p.event(text("c"))
		hooks.OnAssistant(llm.Message{})

		got := strings.Join(record.shapes(), " ")
		if got != "batch:a one: batch:b tool start batch:c reply ended" {
			t.Errorf("sent %s", got)
		}
	})

	t.Run("PC-05 a batch is drawn as its pieces would be, the speeds counting each", func(t *testing.T) {
		m := conversationModel()
		m.running = true
		start := time.Now()
		batch := streamBatchMsg{{text("one "), start}, {text("two\n"), start.Add(50 * time.Millisecond)}, {text("three"), start.Add(100 * time.Millisecond)}}

		updated, _ := m.Update(batch)
		m = updated.(model)

		if m.stream != "one two\nthree" || !strings.Contains(shown2(m), "one two") || m.metrics.pieces != 3 {
			t.Errorf("stream %q pieces %d", m.stream, m.metrics.pieces)
		}
	})

	t.Run("PC-06 the settings show the milliseconds or the tokens as the way chosen needs, and save them", func(t *testing.T) {
		titles := func(refresh string) string {
			menu := &settingsMenu{draft: defaultDraft()}
			menu.draft.replyRefresh = refresh
			var names []string
			for _, item := range replyItems(menu) {
				names = append(names, item.title)
			}
			return strings.Join(names, ", ")
		}
		draft := defaultDraft()
		draft.replyRefresh, draft.replyEveryTokens = config.RefreshTokens, "25"
		layer, err := draft.applyTo(config.Config{}, nil)
		draft.replyEveryMS = "5"
		_, tooFast := draft.applyTo(config.Config{}, nil)

		if titles("live") != "Reply refresh" || !strings.Contains(titles("time"), "(ms)") || !strings.Contains(titles("tokens"), "(tokens)") {
			t.Errorf("live %q time %q tokens %q", titles("live"), titles("time"), titles("tokens"))
		}
		if err != nil || layer.Render.ReplyRefresh != "tokens" || layer.Render.ReplyEveryTokens != 25 || layer.Render.ReplyEveryMS != 0 || tooFast == nil {
			t.Errorf("err %v render %+v too fast %v", err, layer.Render, tooFast)
		}
	})

	t.Run("PC-07 saved while the model works, the refresh applies at once, to the reply being written", func(t *testing.T) {
		m := conversationModel()
		record := &pacedSends{}
		m.bridge = &bridge{pacer: &pacer{send: record.send}}
		m.running, m.cancel = true, func() {}
		fresh := m.session
		fresh.Config.Render.ReplyRefresh, fresh.Config.Render.ReplyEveryMS = config.RefreshTime, 250

		updated, _ := m.finishSettings(settingsSavedMsg{session: fresh, saved: []string{"config.toml"}, waiting: true})

		pace := updated.(model).bridge.pacer.pace
		if updated.(model).pendingSession == nil || pace.mode != config.RefreshTime || pace.every != 250*time.Millisecond {
			t.Errorf("pending %v pace %+v", updated.(model).pendingSession != nil, pace)
		}
	})
}
