package llm

import "time"

// Timing is when what a message holds happened, kept to measure models; it
// is saved with the conversation and never sent.
//
// A prompt starts when it was sent, and ends when the model read it if it
// waited in the queue. A reply starts when it was asked for and ends when
// it ended, with when its first token, its reasoning, its text and its
// calls came, its tokens, and how many times it was asked again. A tool's
// result starts and ends with the call, after the user was asked about it
// and answered.
type Timing struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end,omitzero"`

	FirstToken     time.Time `json:"first_token,omitzero"`
	ReasoningStart time.Time `json:"reasoning_start,omitzero"`
	ReasoningEnd   time.Time `json:"reasoning_end,omitzero"`
	TextStart      time.Time `json:"text_start,omitzero"`
	TextEnd        time.Time `json:"text_end,omitzero"`
	CallsStart     time.Time `json:"calls_start,omitzero"`
	Retries        int       `json:"retries,omitempty"`
	Usage          *Usage    `json:"usage,omitempty"`

	Asked    time.Time `json:"asked,omitzero"`
	Answered time.Time `json:"answered,omitzero"`

	// Summary is when the model wrote the summary a compaction put at the
	// start of this message.
	Summary *Timing `json:"summary,omitempty"`

	// The spans in milliseconds, worked out from the times: from start to
	// end, to the first token, of the reasoning, of the text, of writing
	// the calls, and waiting for the user.
	TotalMS      int64 `json:"total_ms,omitempty"`
	FirstTokenMS int64 `json:"first_token_ms,omitempty"`
	ReasoningMS  int64 `json:"reasoning_ms,omitempty"`
	TextMS       int64 `json:"text_ms,omitempty"`
	CallsMS      int64 `json:"calls_ms,omitempty"`
	WaitedMS     int64 `json:"waited_ms,omitempty"`
}

// StartedNow is a timing that starts now.
func StartedNow() *Timing {
	return &Timing{Start: time.Now()}
}

// Ended sets the end to now, if it has none, and works out the spans.
func (t *Timing) Ended() *Timing {
	if t.End.IsZero() {
		t.End = time.Now()
	}
	t.TotalMS = span(t.Start, t.End)
	t.FirstTokenMS = span(t.Start, t.FirstToken)
	t.ReasoningMS = span(t.ReasoningStart, t.ReasoningEnd)
	t.TextMS = span(t.TextStart, t.TextEnd)
	t.CallsMS = span(t.CallsStart, t.End)
	t.WaitedMS = span(t.Asked, t.Answered)
	return t
}

// Saw notes a piece of a reply as it streams: its kind, at now.
func (t *Timing) Saw(kind EventKind, now time.Time) {
	if kind == EventPromptProgress {
		return
	}
	if t.FirstToken.IsZero() {
		t.FirstToken = now
	}
	switch kind {
	case EventReasoning:
		if t.ReasoningStart.IsZero() {
			t.ReasoningStart = now
		}
		t.ReasoningEnd = now
	case EventText:
		if t.TextStart.IsZero() {
			t.TextStart = now
		}
		t.TextEnd = now
	case EventToolCall:
		if t.CallsStart.IsZero() {
			t.CallsStart = now
		}
	}
}

// span is the milliseconds from one time to another, 0 when either is not
// set.
func span(from, to time.Time) int64 {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return 0
	}
	return to.Sub(from).Milliseconds()
}
