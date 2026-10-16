package render

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | DC-01 | Happy    | Each box of a diagram gets a colour of its own, its text a tint of it, no background | P1  |
// | DC-02 | Happy    | A box holding others is drawn quieter; lines dim, labels light, their spaces kept | P1 |
// | DC-03 | Edge     | A label's letters are never taken for line ends; an ER diagram's marks are | P1      |
// | DC-04 | Happy    | Light backgrounds get colours of their own; the text drawn is unchanged  | P2       |

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// colourAt is the escape sequence in force at the first place text is
// drawn, after the given line's start.
func colourAt(drawn, text string) string {
	for _, line := range strings.Split(drawn, "\n") {
		plain := ansi.Strip(line)
		at := strings.Index(plain, text)
		if at < 0 {
			continue
		}
		column, current := 0, ""
		for i := 0; i < len(line); {
			if strings.HasPrefix(line[i:], "\x1b[") {
				end := strings.IndexByte(line[i:], 'm')
				current = line[i+2 : i+end]
				i += end + 1
				continue
			}
			_, size := []rune(line[i:])[0], len(string([]rune(line[i:])[0]))
			if column == len([]rune(plain[:at])) {
				return current
			}
			column++
			i += size
		}
	}
	return "missing"
}

func TestDiagramColors(t *testing.T) {
	dark := paletteFor(true)

	t.Run("DC-01 each box of a diagram gets a colour of its own, its text a tint of it, no background", func(t *testing.T) {
		drawn := Renderer{Mode: ModeOff, Width: 120, Dark: true}.Mermaid("graph LR\n  A[Alpha] --> B[Beta]\n  B --> C[Gamma]")
		first, second := colourAt(drawn, "┌"), colourAt(drawn, "Beta")

		if first != dark.edges[0].fg() {
			t.Errorf("the first box's edge = %q", first)
		}
		if want := dark.edges[1].mix(dark.text, dark.tint).fg(); second != want {
			t.Errorf("the second box's text = %q, want %q", second, want)
		}
		if strings.Contains(drawn, "48;2;") {
			t.Error("boxes should have no background")
		}
		if strings.Contains(drawn, "```") || ansi.Strip(drawn) == drawn {
			t.Errorf("drawn = %q", drawn)
		}
	})

	t.Run("DC-02 a box holding others is drawn quieter; lines dim, labels light, their spaces kept", func(t *testing.T) {
		drawn := Renderer{Mode: ModeOff, Width: 120, Dark: true}.Mermaid(
			"flowchart LR\n  subgraph G[The group]\n    A[One] -->|real traffic| B[Two]\n  end")
		plain := ansi.Strip(drawn)

		if colourAt(drawn, "The group") != dark.label.fg() || !strings.Contains(plain, "real traffic") || colourAt(drawn, "real traffic") != dark.label.fg() {
			t.Errorf("labels:\n%s", plain)
		}
		if colourAt(drawn, "►") != dark.line.fg() {
			t.Errorf("the arrow = %q", colourAt(drawn, "►"))
		}
		group := findBoxes(gridOf(plain))
		if len(group) != 3 {
			t.Fatalf("boxes = %+v", group)
		}
	})

	t.Run("DC-03 a label's letters are never taken for line ends; an ER diagram's marks are", func(t *testing.T) {
		flow := Renderer{Mode: ModeOff, Width: 120, Dark: true}.Mermaid("graph LR\n  A -->|over love| B")
		er := Renderer{Mode: ModeOff, Width: 120, Dark: true}.Mermaid("erDiagram\n  CUSTOMER ||--o{ ORDER : places")

		if colourAt(flow, "over love") != dark.label.fg() {
			t.Errorf("label = %q in\n%s", colourAt(flow, "over love"), ansi.Strip(flow))
		}
		if colourAt(er, "o{") != dark.line.fg() || colourAt(er, "places") != dark.label.fg() {
			t.Errorf("marks = %q places = %q in\n%s", colourAt(er, "o{"), colourAt(er, "places"), ansi.Strip(er))
		}
	})

	t.Run("DC-04 light backgrounds get colours of their own; the text drawn is unchanged", func(t *testing.T) {
		source := "graph LR\n  A[Alpha] --> B[Beta]"
		light := Renderer{Mode: ModeOff, Width: 120}.Mermaid(source)
		plain, _ := mermaidText(source)

		if colourAt(light, "┌") != paletteFor(false).edges[0].fg() {
			t.Errorf("light edge = %q", colourAt(light, "┌"))
		}
		if ansi.Strip(light) != strings.TrimRight(plain, "\n") {
			t.Errorf("text changed:\n%s\n---\n%s", ansi.Strip(light), plain)
		}
	})
}

func gridOf(text string) [][]rune {
	var grid [][]rune
	for _, line := range strings.Split(text, "\n") {
		grid = append(grid, []rune(line))
	}
	return grid
}
