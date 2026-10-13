package agent

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jg/mo/internal/llm"
)

const (
	shortestRepeat        = 16
	longestRepeat         = 2000
	lettersInRepeat       = 8
	fewestLettersInRepeat = 2
	fewLettersCopiesTimes = 4
)

// repeatWatch follows a streamed reply and spots when its end is one piece,
// a phrase, a line or a paragraph, written several times in a row.
// Text and reasoning are followed apart.
type repeatWatch struct {
	times int
	texts map[llm.EventKind]string
}

func newRepeatWatch(times int) *repeatWatch {
	return &repeatWatch{times: times, texts: map[llm.EventKind]string{}}
}

// add takes the next delta and returns the repeated piece once the end of
// the stream repeats, or "" while it does not.
func (w *repeatWatch) add(event llm.Event) string {
	if event.Kind == llm.EventToolCall || event.Kind == llm.EventPromptProgress {
		return ""
	}
	text := w.texts[event.Kind] + event.Text
	if keep := w.times * fewLettersCopiesTimes * longestRepeat; len(text) > keep {
		text = text[len(text)-keep:]
	}
	w.texts[event.Kind] = text
	return repeatedEnd(text, w.times)
}

// repeatedEnd returns the shortest piece that text ends with times in a row.
// A piece starts on a whole character and holds at least lettersInRepeat
// different letters; one with fewer, down to fewestLettersInRepeat, needs
// fewLettersCopiesTimes as many copies.
func repeatedEnd(text string, times int) string {
	letters := distinctLettersFromEnd(text, min(longestRepeat, len(text)))
	for size := shortestRepeat; size <= longestRepeat && size*times <= len(text); size++ {
		copies := times
		if letters[size] < lettersInRepeat {
			copies = times * fewLettersCopiesTimes
		}
		if letters[size] < fewestLettersInRepeat || size*copies > len(text) || !utf8.RuneStart(text[len(text)-size]) {
			continue
		}
		if endsWithCopies(text, text[len(text)-size:], copies) {
			return pieceAtRunStart(text, size)
		}
	}
	return ""
}

// distinctLettersFromEnd returns, for every length up to limit, how many
// different letters the last that many bytes of text hold, counting up to
// lettersInRepeat.
func distinctLettersFromEnd(text string, limit int) []int {
	counts := make([]int, limit+1)
	var seen []rune
	for size := 1; size <= limit; size++ {
		start := len(text) - size
		if len(seen) < lettersInRepeat && utf8.RuneStart(text[start]) {
			character, _ := utf8.DecodeRuneInString(text[start:])
			if unicode.IsLetter(character) && !slices.Contains(seen, character) {
				seen = append(seen, character)
			}
		}
		counts[size] = len(seen)
	}
	return counts
}

// pieceAtRunStart returns the repeated piece as it reads where its run of
// copies begins, from the start of a line when it holds a line break.
func pieceAtRunStart(text string, size int) string {
	start := len(text) - size
	for start > 0 && text[start-1] == text[start-1+size] {
		start--
	}
	if lineBreak := strings.IndexByte(text[start:start+size], '\n'); start > 0 && text[start-1] != '\n' && lineBreak >= 0 {
		start += lineBreak + 1
	}
	for !utf8.RuneStart(text[start]) {
		start++
	}
	return text[start : start+size]
}

func endsWithCopies(text, piece string, times int) bool {
	end := len(text) - len(piece)
	for range times - 1 {
		if text[end-len(piece):end] != piece {
			return false
		}
		end -= len(piece)
	}
	return true
}
