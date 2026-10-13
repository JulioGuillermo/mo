package llm

import "strings"

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// thinkSplitter takes out the reasoning some models write into the answer
// itself, between <think> and </think> at its very start, so it is shown and
// kept as reasoning. A tag split across deltas is held until it is whole.
type thinkSplitter struct {
	state   int
	pending string
}

const (
	thinkStart = iota
	thinkInside
	thinkDone
)

// feed takes one delta of the answer and returns what of it is answer and
// what is reasoning.
func (s *thinkSplitter) feed(text string) (answer, reasoning string) {
	switch s.state {
	case thinkStart:
		s.pending += text
		trimmed := strings.TrimLeft(s.pending, " \t\r\n")
		switch {
		case trimmed == "" || strings.HasPrefix(thinkOpen, trimmed):
			return "", ""
		case strings.HasPrefix(trimmed, thinkOpen):
			s.state, s.pending = thinkInside, ""
			return s.feed(trimmed[len(thinkOpen):])
		}
		s.state = thinkDone
		answer, s.pending = s.pending, ""
		return answer, ""
	case thinkInside:
		s.pending += text
		if end := strings.Index(s.pending, thinkClose); end >= 0 {
			reasoning, answer = s.pending[:end], strings.TrimLeft(s.pending[end+len(thinkClose):], "\r\n")
			s.state, s.pending = thinkDone, ""
			return answer, reasoning
		}
		keep := partialSuffix(s.pending, thinkClose)
		reasoning, s.pending = s.pending[:len(s.pending)-keep], s.pending[len(s.pending)-keep:]
		return "", reasoning
	}
	return text, ""
}

// flush returns what is still held when the answer ends.
func (s *thinkSplitter) flush() (answer, reasoning string) {
	pending := s.pending
	s.pending = ""
	if s.state == thinkInside {
		return "", pending
	}
	return pending, ""
}

// partialSuffix is the length of the longest end of text that starts tag.
func partialSuffix(text, tag string) int {
	for n := min(len(tag)-1, len(text)); n > 0; n-- {
		if strings.HasSuffix(text, tag[:n]) {
			return n
		}
	}
	return 0
}
