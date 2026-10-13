package gitops

import (
	"slices"
	"strings"
)

// Conflict markers, as git writes them with diff3 style: a reader sees what
// each side and the common base had.
const (
	markerOurs   = "<<<<<<< "
	markerBase   = "||||||| "
	markerSplit  = "======="
	markerTheirs = ">>>>>>> "
)

// Merge3 merges what ours and theirs each changed from base, line by line.
// A part both changed in different ways is a conflict, written between
// markers labelled with the names given; the result says whether any was.
func Merge3(base, ours, theirs, oursName, baseName, theirsName string) (string, bool) {
	b, o, t := lines(base), lines(ours), lines(theirs)
	toOurs, toTheirs := matches(b, o), matches(b, t)
	var out strings.Builder
	conflicted := false
	i, j, k := 0, 0, 0
	for {
		stable := i
		for stable < len(b) && (toOurs[stable] < j || toTheirs[stable] < k) {
			stable++
		}
		oursEnd, theirsEnd := len(o), len(t)
		if stable < len(b) {
			oursEnd, theirsEnd = toOurs[stable], toTheirs[stable]
		}
		baseRegion, oursRegion, theirsRegion := b[i:stable], o[j:oursEnd], t[k:theirsEnd]
		switch {
		case slices.Equal(oursRegion, baseRegion):
			writeAll(&out, theirsRegion)
		case slices.Equal(theirsRegion, baseRegion), slices.Equal(oursRegion, theirsRegion):
			writeAll(&out, oursRegion)
		default:
			conflicted = true
			out.WriteString(markerOurs + oursName + "\n")
			writeEnded(&out, oursRegion)
			out.WriteString(markerBase + baseName + "\n")
			writeEnded(&out, baseRegion)
			out.WriteString(markerSplit + "\n")
			writeEnded(&out, theirsRegion)
			out.WriteString(markerTheirs + theirsName + "\n")
		}
		if stable >= len(b) {
			break
		}
		out.WriteString(b[stable])
		i, j, k = stable+1, toOurs[stable]+1, toTheirs[stable]+1
	}
	return out.String(), conflicted
}

func writeAll(out *strings.Builder, region []string) {
	for _, line := range region {
		out.WriteString(line)
	}
}

// writeEnded writes a region inside markers, ending its last line so the
// next marker starts on a line of its own.
func writeEnded(out *strings.Builder, region []string) {
	writeAll(out, region)
	if len(region) > 0 && !strings.HasSuffix(region[len(region)-1], "\n") {
		out.WriteString("\n")
	}
}

// Marker is where a conflict starts in a file: its line, from 1.
type Marker struct {
	Line int
}

// Markers finds the conflicts left in a text: a line starting the ours
// marker, followed by a split line and a theirs marker. A lone "=======",
// as a markdown heading has, is not one.
func Markers(text string) []Marker {
	var found []Marker
	start, split := 0, false
	for number, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, markerOurs):
			start, split = number+1, false
		case start > 0 && line == markerSplit:
			split = true
		case start > 0 && split && strings.HasPrefix(line, markerTheirs):
			found = append(found, Marker{Line: start})
			start, split = 0, false
		}
	}
	return found
}
