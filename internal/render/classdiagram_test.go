package render

// Test Traceability Table
// | ID    | Category | Description                                                                | Priority |
// | CD-01 | Happy    | A class diagram is drawn as boxes of name, attributes and methods parted by rules | P1 |
// | CD-02 | Happy    | Relations point as their kind says, with cardinalities and labels; ◆ and ◇ mark parts | P1 |
// | CD-03 | Edge     | An empty class is its name alone; an empty part keeps a line; no blank lines between members | P2 |
// | CD-04 | Happy    | Flowchart links mermaid-ascii does not read are written as ones it does   | P1       |

import (
	"strings"
	"testing"
)

func TestClassDiagrams(t *testing.T) {
	t.Run("CD-01 a class diagram is drawn as boxes of name, attributes and methods parted by rules", func(t *testing.T) {
		drawn, err := mermaidText("classDiagram\n  class Profile {\n    +monitor : boolean\n    +scope()\n  }\n  <<interface>> Profile")
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(drawn, "\n")
		find := func(text string) int {
			for i, line := range lines {
				if strings.Contains(line, text) {
					return i
				}
			}
			return -1
		}
		annotation, name, attribute, method := find("«interface»"), find("Profile"), find("+monitor : boolean"), find("+scope()")
		if annotation < 0 || name != annotation+1 || attribute != name+2 || method != attribute+2 {
			t.Fatalf("rows: %d %d %d %d in\n%s", annotation, name, attribute, method, drawn)
		}
		if !strings.HasPrefix(strings.TrimSpace(lines[name+1]), "├") || !strings.HasPrefix(strings.TrimSpace(lines[attribute+1]), "├") {
			t.Errorf("rules missing:\n%s", drawn)
		}
		if !strings.Contains(lines[attribute], "│+monitor") {
			t.Errorf("members should sit on the left:\n%s", drawn)
		}
	})

	t.Run("CD-02 relations point as their kind says, with cardinalities and labels; ◆ and ◇ mark parts", func(t *testing.T) {
		for want, relation := range map[string]string{
			"c1 ==> c0":           "A <|-- B",
			"c0 ==> c1":           "A --|> B",
			"c0 -.-> c1":          "A ..|> B",
			"c1 -.-> c0":          "A <.. B",
			"c0 -->|1 has *| c1":  `A "1" --> "*" B : has`,
			"c1 -->|* owns 1| c0": `A "1" <-- "*" B : owns`,
			"c0 ---|◆ parts| c1":  "A *-- B : parts",
			"c0 ---|◇| c1":        "A o-- B",
			"c0 --- c1":           "A .. B",
		} {
			flowchart, err := classFlowchart("classDiagram\n  " + relation)
			if err != nil || !strings.Contains(flowchart, "  "+want+"\n") {
				t.Errorf("%s: want %q in\n%s (%v)", relation, want, flowchart, err)
			}
		}
	})

	t.Run("CD-03 an empty class is its name alone; an empty part keeps a line; no blank lines between members", func(t *testing.T) {
		empty, _ := mermaidText("classDiagram\n  class Lonely")
		half, _ := mermaidText("classDiagram\n  class Half {\n    +a()\n    +b()\n  }")
		lines := strings.Split(half, "\n")
		at := -1
		for i, line := range lines {
			if strings.Contains(line, "+a()") {
				at = i
			}
		}

		if strings.Contains(empty, "├") || !strings.Contains(empty, "Lonely") {
			t.Errorf("empty:\n%s", empty)
		}
		if at < 3 || !strings.Contains(lines[at+1], "+b()") || !strings.Contains(lines[at-1], "├") ||
			strings.TrimSpace(strings.Trim(lines[at-2], " │")) != "" || !strings.Contains(lines[at-3], "├") {
			t.Errorf("half:\n%s", half)
		}
		if strings.ContainsRune(half, '⠀') {
			t.Error("the filler of an empty part should be drawn as a space")
		}
	})

	t.Run("CD-04 flowchart links mermaid-ascii does not read are written as ones it does", func(t *testing.T) {
		for source, want := range map[string]string{
			"A -- some words --> B":   "A -->|some words| B",
			"A -- some words --- B":   "A ---|some words| B",
			"A == thick words ==> B":  "A ==>|thick words| B",
			"A -. dotted words .-> B": "A -.->|dotted words| B",
			"A -.- B":                 "A --- B",
			"A <-.-> B":               "A <--> B",
			"A <-- both ways --> B":   "A <-->|both ways| B",
			"A ---> B":                "A --> B",
			"A ~~~ B":                 "A\n  B",
			"A -->|kept| B":           "A -->|kept| B",
		} {
			if got := readableFlowchart("graph LR\n  " + source); got != "graph LR\n  "+want {
				t.Errorf("%s: got %q, want %q", source, got, "graph LR\n  "+want)
			}
		}
		if sequence := "sequenceDiagram\n  A -- B: hi"; readableFlowchart(sequence) != sequence {
			t.Error("other diagrams should stay as they are")
		}
		drawn, _ := mermaidText("graph LR\n  A -. copy .-> B")
		if strings.Contains(drawn, "-.") || !strings.Contains(drawn, "copy") {
			t.Errorf("drawn:\n%s", drawn)
		}
	})
}
