package render

import (
	"fmt"
	"regexp"
	"strings"
)

// classSeparator is the line of a class's label that parts its name,
// attributes and methods, drawn as a rule across its box afterwards;
// emptyCompartment holds the room of a part with nothing in it, a blank
// that is not taken for space, so the line stays.
const (
	classSeparator   = "─"
	emptyCompartment = "\u2800"
)

var (
	classHeader   = regexp.MustCompile(`^classDiagram(-v2)?\s*$`)
	classOpen     = regexp.MustCompile(`^class\s+([^\s{~]+)(~[^~]+~)?\s*(\[[^\]]*\])?\s*(\{)?\s*$`)
	classMember   = regexp.MustCompile(`^([^\s:]+)\s*:\s*(.+)$`)
	classNote     = regexp.MustCompile(`^(note|style|classDef|cssClass|click|link|callback)\b`)
	annotationFor = regexp.MustCompile(`^<<([^>]+)>>\s*(\S+)?$`)
	// A relation: from, its cardinality, the link, the other's cardinality,
	// to, and a label.
	classRelation = regexp.MustCompile(`^(\S+?)\s*(?:"([^"]*)")?\s*(<\|--|<\|\.\.|--\|>|\.\.\|>|\*--|--\*|o--|--o|<--|-->|<\.\.|\.\.>|--|\.\.)\s*(?:"([^"]*)")?\s*([^\s:]+)\s*(?::\s*(.*))?$`)
	classGenerics = regexp.MustCompile(`~([^~]*)~`)
)

// classBox is what a class diagram says of one class.
type classBox struct {
	id, name    string
	annotations []string
	attributes  []string
	methods     []string
}

// isClassDiagram tells a mermaid class diagram.
func isClassDiagram(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "%%") {
			continue
		}
		return classHeader.MatchString(trimmed)
	}
	return false
}

// classFlowchart turns a class diagram into a flowchart mermaid-ascii can
// lay out: each class a node of its name, attributes and methods parted
// by separator lines, each relation an edge whose look and label say its
// kind and cardinalities.
func classFlowchart(source string) (string, error) {
	classes := map[string]*classBox{}
	var order []string
	get := func(name string) *classBox {
		name = classGenerics.ReplaceAllString(strings.Trim(name, "`"), "<$1>")
		key := strings.SplitN(name, "<", 2)[0]
		if found, ok := classes[key]; ok {
			return found
		}
		box := &classBox{id: fmt.Sprintf("c%d", len(order)), name: name}
		classes[key] = box
		order = append(order, key)
		return box
	}
	addMember := func(box *classBox, member string) {
		member = strings.TrimSpace(classGenerics.ReplaceAllString(member, "<$1>"))
		switch {
		case member == "":
		case strings.HasPrefix(member, "<<") && strings.HasSuffix(member, ">>"):
			box.annotations = append(box.annotations, "«"+strings.Trim(member, "<>")+"»")
		case strings.Contains(member, "("):
			box.methods = append(box.methods, member)
		default:
			box.attributes = append(box.attributes, member)
		}
	}
	direction := "TD"
	var edges []string
	var open *classBox
	header := false
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		if !header {
			if !classHeader.MatchString(line) {
				return "", fmt.Errorf("not a class diagram")
			}
			header = true
			continue
		}
		if open != nil {
			if line == "}" {
				open = nil
				continue
			}
			addMember(open, line)
			continue
		}
		switch {
		case strings.HasPrefix(line, "direction "):
			switch strings.TrimSpace(strings.TrimPrefix(line, "direction ")) {
			case "LR", "RL":
				direction = "LR"
			}
		case classNote.MatchString(line), strings.HasPrefix(line, "namespace "), line == "}":
		case annotationFor.MatchString(line):
			parts := annotationFor.FindStringSubmatch(line)
			if parts[2] != "" {
				get(parts[2]).annotations = append(get(parts[2]).annotations, "«"+parts[1]+"»")
			}
		case classOpen.MatchString(line):
			parts := classOpen.FindStringSubmatch(line)
			box := get(parts[1] + parts[2])
			if parts[4] == "{" {
				open = box
			}
		case classRelation.MatchString(line):
			parts := classRelation.FindStringSubmatch(line)
			edges = append(edges, classEdge(get(parts[1]), get(parts[5]), parts[3], parts[2], parts[4], parts[6]))
		case classMember.MatchString(line):
			parts := classMember.FindStringSubmatch(line)
			addMember(get(parts[1]), parts[2])
		}
	}
	if len(order) == 0 {
		return "", fmt.Errorf("no classes")
	}
	var out strings.Builder
	out.WriteString("graph " + direction + "\n")
	for _, key := range order {
		box := classes[key]
		lines := append(append([]string{}, box.annotations...), box.name)
		if len(box.attributes)+len(box.methods) > 0 {
			for _, part := range [][]string{box.attributes, box.methods} {
				lines = append(lines, classSeparator)
				if len(part) == 0 {
					part = []string{emptyCompartment}
				}
				lines = append(lines, part...)
			}
		}
		for i, line := range lines {
			lines[i] = strings.NewReplacer(`"`, "'", "[", "(", "]", ")").Replace(line)
		}
		fmt.Fprintf(&out, "  %s[\"%s\"]\n", box.id, strings.Join(lines, "<br>"))
	}
	for _, edge := range edges {
		out.WriteString("  " + edge + "\n")
	}
	return out.String(), nil
}

// classEdge is the flowchart edge of a relation: the arrow to whom it
// points, thick for inheritance, dotted for realization and dependency,
// a plain line with ◆ or ◇ for composition and aggregation; the label its
// own words between the cardinalities.
func classEdge(from, to *classBox, link, fromCard, toCard, label string) string {
	arrow, mark := "-->", ""
	reversed := false
	switch link {
	case "<|--":
		arrow, reversed = "==>", true
	case "--|>":
		arrow = "==>"
	case "<|..":
		arrow, reversed = "-.->", true
	case "..|>", "..>":
		arrow = "-.->"
	case "<..":
		arrow, reversed = "-.->", true
	case "<--":
		reversed = true
	case "*--", "--*":
		arrow, mark = "---", "◆"
	case "o--", "--o":
		arrow, mark = "---", "◇"
	case "--", "..":
		arrow = "---"
	}
	var words []string
	for _, word := range []string{fromCard, mark, strings.TrimSpace(label), toCard} {
		if word != "" {
			words = append(words, word)
		}
	}
	if reversed {
		from, to = to, from
		for i, j := 0, len(words)-1; i < j; i, j = i+1, j-1 {
			words[i], words[j] = words[j], words[i]
		}
	}
	text := strings.NewReplacer("|", "/", `"`, "'").Replace(strings.Join(words, " "))
	if text == "" {
		return from.id + " " + arrow + " " + to.id
	}
	return from.id + " " + arrow + "|" + text + "| " + to.id
}

// tidyClassBoxes makes the boxes of a class diagram drawn by mermaid-ascii
// read as classes: a separator line becomes a rule across its box, the
// blank lines it puts between a label's lines go, and the members line up
// on the left.
func tidyClassBoxes(drawn string) string {
	lines := strings.Split(strings.TrimRight(drawn, "\n"), "\n")
	grid := make([][]rune, len(lines))
	for i, line := range lines {
		grid[i] = []rune(line)
	}
	boxes := findBoxes(grid)
	interior := func(box diagramBox, row int) string {
		return string(grid[row][box.left+1 : box.right])
	}
	removable := map[int]bool{}
	for _, box := range boxes {
		content := func(row int) bool { return strings.TrimSpace(interior(box, row)) != "" }
		ruled := false
		for row := box.top + 1; row < box.bottom; row++ {
			text := strings.TrimSpace(interior(box, row))
			switch {
			case text == classSeparator:
				grid[row][box.left], grid[row][box.right] = '├', '┤'
				for column := box.left + 1; column < box.right; column++ {
					grid[row][column] = '─'
				}
				ruled = true
			case text != "" && ruled:
				// A member: on the left.
				width := box.right - box.left - 1
				left := text
				if len([]rune(left)) <= width {
					copy(grid[row][box.left+1:box.right], []rune(left+strings.Repeat(" ", width-len([]rune(left)))))
				}
			case text == "":
				above, below := false, false
				for up := row - 1; up > box.top; up-- {
					if content(up) {
						above = true
						break
					}
				}
				for down := row + 1; down < box.bottom; down++ {
					if content(down) {
						below = true
						break
					}
				}
				if above && below {
					removable[row] = true
				}
			}
		}
	}
	var out []string
	for row, line := range grid {
		if removable[row] && onlyVertical(line) {
			continue
		}
		out = append(out, strings.ReplaceAll(string(line), emptyCompartment, " "))
	}
	return strings.Join(out, "\n")
}

// onlyVertical tells a row holding nothing but spaces and vertical lines,
// which can go without breaking what it crosses.
func onlyVertical(line []rune) bool {
	for _, character := range line {
		if character != ' ' && !strings.ContainsRune("│┆┊╎║┃", character) {
			return false
		}
	}
	return true
}
