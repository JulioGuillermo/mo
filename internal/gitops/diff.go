// Package gitops works on git repositories in pure Go, on top of go-git:
// reading their state, committing, and merging and cherry-picking with the
// three-way merge go-git lacks, leaving conflicts for the model to resolve.
package gitops

import (
	"fmt"
	"strings"
)

const contextLines = 3

// lines splits text into lines that keep their line ends, so joining them
// gives the text back.
func lines(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for line := range strings.Lines(text) {
		out = append(out, line)
	}
	return out
}

// matches pairs the lines of a and b that a shortest edit script keeps,
// in order: match[i] is the line of b that a[i] stays as, or -1. Myers'
// algorithm, O((N+M)D).
func matches(a, b []string) []int {
	n, m := len(a), len(b)
	match := make([]int, n)
	for i := range match {
		match[i] = -1
	}
	if n == 0 || m == 0 {
		return match
	}
	most := n + m
	offset := most
	v := make([]int, 2*most+2)
	var trace [][]int
	found := false
	for d := 0; d <= most && !found; d++ {
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[offset+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	x, y := n, m
	for d := len(trace) - 1; d >= 0 && (x > 0 || y > 0); d-- {
		before := trace[d]
		k := x - y
		var previous int
		if k == -d || (k != d && before[offset+k-1] < before[offset+k+1]) {
			previous = k + 1
		} else {
			previous = k - 1
		}
		startX := before[offset+previous]
		startY := startX - previous
		for x > startX && y > startY {
			x, y = x-1, y-1
			match[x] = y
		}
		if d > 0 {
			x, y = startX, startY
		}
	}
	return match
}

// Unified writes the changes from a to b as a unified diff of the named
// file, with three lines of context; empty when nothing changed.
func Unified(name, a, b string) string {
	before, after := lines(a), lines(b)
	match := matches(before, after)
	type edit struct {
		kind byte
		text string
		i, j int
	}
	var edits []edit
	i, j := 0, 0
	for i < len(before) || j < len(after) {
		switch {
		case i < len(before) && match[i] == j:
			edits = append(edits, edit{' ', before[i], i, j})
			i, j = i+1, j+1
		case i < len(before) && match[i] < 0:
			edits = append(edits, edit{'-', before[i], i, j})
			i++
		default:
			edits = append(edits, edit{'+', after[j], i, j})
			j++
		}
	}
	var out strings.Builder
	for start := 0; start < len(edits); {
		if edits[start].kind == ' ' {
			start++
			continue
		}
		from := max(start-contextLines, 0)
		end := start
		for quiet := 0; end < len(edits) && quiet <= 2*contextLines; end++ {
			if edits[end].kind == ' ' {
				quiet++
			} else {
				quiet = 0
			}
		}
		end = min(end, len(edits))
		for end > start && edits[end-1].kind == ' ' && end-start > contextLines {
			trailing := 0
			for t := end - 1; t >= start && edits[t].kind == ' '; t-- {
				trailing++
			}
			if trailing <= contextLines {
				break
			}
			end--
		}
		oldCount, newCount := 0, 0
		for _, e := range edits[from:end] {
			if e.kind != '+' {
				oldCount++
			}
			if e.kind != '-' {
				newCount++
			}
		}
		if out.Len() == 0 {
			fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", name, name)
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", edits[from].i+1, oldCount, edits[from].j+1, newCount)
		for _, e := range edits[from:end] {
			out.WriteByte(e.kind)
			out.WriteString(e.text)
			if !strings.HasSuffix(e.text, "\n") {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}
		start = end
	}
	return out.String()
}
