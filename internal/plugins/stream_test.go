package plugins

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | ST-01 | Happy    | A piece after a quiet gap goes at once; those that come within the gap wait and go together | P0 |
// | ST-02 | Happy    | Pieces further apart than the gap go one by one                           | P1       |
// | ST-03 | Happy    | Text and reasoning go apart in the same call                              | P1       |
// | ST-04 | Edge     | A slow hook does not slow down who adds the pieces                        | P0       |
// | ST-05 | Happy    | A stream hook of a plugin gets what the reply streamed, all of it, in order | P0     |

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jg/mo/internal/llm"
)

// recordedSends keeps what a feed sent, and when.
type recordedSends struct {
	mu    sync.Mutex
	texts []string
	at    []time.Time
}

func (r *recordedSends) send(text, reasoning string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, text+"|"+reasoning)
	r.at = append(r.at, time.Now())
}

func (r *recordedSends) get() ([]string, []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.texts...), append([]time.Time(nil), r.at...)
}

func TestStreamFeed(t *testing.T) {
	const gap = 100 * time.Millisecond

	t.Run("ST-01 a piece after a quiet gap goes at once; those that come within the gap wait and go together", func(t *testing.T) {
		var sends recordedSends
		feed := newStreamFeed(gap, sends.send)
		start := time.Now()

		feed.add(llm.EventText, "a")
		time.Sleep(10 * time.Millisecond)
		for _, piece := range []string{"b", "c", "d"} {
			feed.add(llm.EventText, piece)
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(2 * gap)

		texts, at := sends.get()
		if strings.Join(texts, ",") != "a|,bcd|" {
			t.Fatalf("texts = %q", texts)
		}
		if at[0].Sub(start) > gap/2 || at[1].Sub(at[0]) < gap-10*time.Millisecond {
			t.Errorf("first after %v, second %v after it", at[0].Sub(start), at[1].Sub(at[0]))
		}
	})

	t.Run("ST-02 pieces further apart than the gap go one by one", func(t *testing.T) {
		var sends recordedSends
		feed := newStreamFeed(gap, sends.send)

		for _, piece := range []string{"x", "y", "z"} {
			feed.add(llm.EventText, piece)
			time.Sleep(gap + 50*time.Millisecond)
		}

		if texts, _ := sends.get(); strings.Join(texts, ",") != "x|,y|,z|" {
			t.Errorf("texts = %q", texts)
		}
	})

	t.Run("ST-03 text and reasoning go apart in the same call", func(t *testing.T) {
		var sends recordedSends
		feed := newStreamFeed(gap, sends.send)

		feed.add(llm.EventReasoning, "think")
		feed.add(llm.EventText, "say")
		feed.add(llm.EventReasoning, "ing")
		time.Sleep(2 * gap)

		if texts, _ := sends.get(); strings.Join(texts, ",") != "say|thinking" {
			t.Errorf("texts = %q", texts)
		}
	})

	t.Run("ST-04 a slow hook does not slow down who adds the pieces", func(t *testing.T) {
		feed := newStreamFeed(gap, func(string, string) { time.Sleep(time.Second) })
		start := time.Now()

		for range 1000 {
			feed.add(llm.EventText, "t")
		}

		if spent := time.Since(start); spent > 100*time.Millisecond {
			t.Errorf("adding took %v", spent)
		}
	})

	t.Run("ST-05 a stream hook of a plugin gets what the reply streamed, all of it, in order", func(t *testing.T) {
		f := loadSource(t, map[string]string{"watch": `
local seen = {}
mo.on("stream", function(event)
  seen[#seen + 1] = event.text
  mo.sidebar.set{ id = "seen", text = table.concat(seen, "") }
end)`})
		pieces := strings.Split("the quick brown fox jumps over the lazy dog", "")

		for _, piece := range pieces {
			f.set.StreamPiece(llm.EventText, piece)
			time.Sleep(5 * time.Millisecond)
		}

		eventually(t, "the hook saw the whole reply", func() bool {
			items := f.set.SidebarItems()
			return len(items) == 1 && items[0].Text == strings.Join(pieces, "")
		})
	})
}
