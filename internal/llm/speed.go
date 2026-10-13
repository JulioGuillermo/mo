package llm

import "time"

// speedClock times a streamed reply: from the request to its first piece,
// and from there to the end.
type speedClock struct {
	start time.Time
	first time.Time
}

func newSpeedClock() *speedClock {
	return &speedClock{start: time.Now()}
}

func (c *speedClock) piece() {
	if c.first.IsZero() {
		c.first = time.Now()
	}
}

// fill sets the rates the server did not report from the times measured.
func (c *speedClock) fill(usage Usage) Usage {
	if c.first.IsZero() {
		return usage
	}
	reading := c.first.Sub(c.start).Seconds()
	writing := time.Since(c.first).Seconds()
	if usage.PromptRate == 0 && reading > 0 && usage.InputTokens > 0 {
		usage.PromptRate = float64(usage.InputTokens) / reading
	}
	if usage.GenerationRate == 0 && writing > 0 && usage.OutputTokens > 0 {
		usage.GenerationRate = float64(usage.OutputTokens) / writing
	}
	return usage
}
