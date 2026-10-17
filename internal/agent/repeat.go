package agent

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jg/mo/internal/llm"
)

const (
	// maxStream is how much of a stream is kept to look for a repeat in.
	maxStream = 1 << 20
	// keyLength is the end of the stream looked up in what came before, to
	// find the places where it was written already.
	keyLength     = 8
	hashBits      = 16
	maxCandidates = 4096
	// lettersInRepeat different letters make a piece rich: it is a loop after
	// times copies. A poorer piece, as "a", "Wait, " or "0, ", is one that
	// legitimate text repeats too, so it is one after poorSpan characters.
	lettersInRepeat = 8
	poorSpan        = 1000
)

// repeatWatch follows a streamed reply and spots when its end is one piece,
// a phrase, a line or a paragraph, written several times in a row.
// Text and reasoning are followed apart.
type repeatWatch struct {
	times   int
	streams map[llm.EventKind]*repeatStream
}

func newRepeatWatch(times int) *repeatWatch {
	return &repeatWatch{times: times, streams: map[llm.EventKind]*repeatStream{}}
}

// add takes the next delta and returns the repeated piece once the end of
// the stream repeats, or "" while it does not.
func (w *repeatWatch) add(event llm.Event) string {
	if event.Kind == llm.EventToolCall || event.Kind == llm.EventPromptProgress {
		return ""
	}
	stream := w.streams[event.Kind]
	if stream == nil {
		stream = newRepeatStream()
		w.streams[event.Kind] = stream
	}
	stream.write(event.Text)
	return stream.repeatedEnd(w.times)
}

// repeatedEnd returns the shortest piece that text ends with times in a row.
func repeatedEnd(text string, times int) string {
	stream := newRepeatStream()
	stream.write(text)
	return stream.repeatedEnd(times)
}

// repeatStream keeps the bytes of one stream and, as the compressors do, a
// chain through the places where the same keyLength bytes were written, so
// the end of the stream finds where it came before without looking at each
// size of piece.
type repeatStream struct {
	buf  []byte
	head [1 << hashBits]int32
	// prev holds, for each position, the one before it with the same key, +1;
	// head the last one for each key.
	prev []int32
}

func newRepeatStream() *repeatStream {
	return &repeatStream{}
}

func keyHash(key []byte) uint32 {
	return uint32(binary.LittleEndian.Uint64(key) * 0x9E3779B97F4A7C15 >> (64 - hashBits))
}

func (s *repeatStream) write(text string) {
	for i := 0; i < len(text); i++ {
		s.buf = append(s.buf, text[i])
		s.prev = append(s.prev, 0)
		if last := len(s.buf) - 1; last >= keyLength-1 {
			hash := keyHash(s.buf[last-keyLength+1 : last+1])
			s.prev[last] = s.head[hash]
			s.head[hash] = int32(last + 1)
		}
	}
	if len(s.buf) > 2*maxStream {
		s.trim()
	}
}

// trim keeps the last maxStream bytes and indexes them again.
func (s *repeatStream) trim() {
	kept := copy(s.buf, s.buf[len(s.buf)-maxStream:])
	s.buf, s.prev = s.buf[:kept], s.prev[:kept]
	s.head = [1 << hashBits]int32{}
	for last := range s.buf {
		s.prev[last] = 0
		if last >= keyLength-1 {
			hash := keyHash(s.buf[last-keyLength+1 : last+1])
			s.prev[last] = s.head[hash]
			s.head[hash] = int32(last + 1)
		}
	}
}

// repeatedEnd looks at where the last bytes were written before: the distance
// to each is a period, and the end is a loop when it holds the same piece
// again and again over it. The nearest periods come first, so the piece is the
// shortest. A piece starts on a whole character.
func (s *repeatStream) repeatedEnd(times int) string {
	end := len(s.buf)
	if end < keyLength {
		return ""
	}
	last := end - 1
	candidate := s.prev[last]
	for range maxCandidates {
		if candidate == 0 {
			return ""
		}
		period := last - (int(candidate) - 1)
		candidate = s.prev[candidate-1]
		if 2*period > end {
			return ""
		}
		if !utf8.RuneStart(s.buf[end-period]) {
			continue
		}
		piece := s.buf[end-period:]
		rich := distinctLetters(piece) >= lettersInRepeat
		limit := poorSpan * utf8.UTFMax
		if rich {
			limit = times * period
		}
		run := commonRun(s.buf, period, min(limit, end))
		span := run + period
		switch {
		case run < period:
			continue
		case rich && span >= times*period:
		case !rich && span >= poorSpan && utf8.RuneCount(s.buf[end-span:]) >= poorSpan:
		default:
			continue
		}
		return s.pieceAtRunStart(period)
	}
	return ""
}

// commonRun counts how many bytes from the end, up to limit, equal the ones a
// period before them.
func commonRun(buf []byte, period, limit int) int {
	end := len(buf)
	run := 0
	for run < limit-period && end-1-run-period >= 0 && buf[end-1-run] == buf[end-1-run-period] {
		run++
	}
	return run
}

// distinctLetters counts the different letters of a piece, up to
// lettersInRepeat.
func distinctLetters(piece []byte) int {
	var seen []rune
	for len(piece) > 0 && len(seen) < lettersInRepeat {
		character, size := utf8.DecodeRune(piece)
		piece = piece[size:]
		if unicode.IsLetter(character) && !containsRune(seen, character) {
			seen = append(seen, character)
		}
	}
	return len(seen)
}

func containsRune(runes []rune, wanted rune) bool {
	for _, character := range runes {
		if character == wanted {
			return true
		}
	}
	return false
}

// pieceAtRunStart returns the repeated piece as it reads where its run of
// copies begins, from the start of a line when it holds a line break.
func (s *repeatStream) pieceAtRunStart(period int) string {
	text := s.buf
	start := len(text) - period
	for start > 0 && text[start-1] == text[start-1+period] {
		start--
	}
	if lineBreak := bytes.IndexByte(text[start:start+period], '\n'); start > 0 && text[start-1] != '\n' && lineBreak >= 0 {
		start += lineBreak + 1
	}
	for !utf8.RuneStart(text[start]) {
		start++
	}
	return strings.Clone(string(text[start : start+period]))
}
