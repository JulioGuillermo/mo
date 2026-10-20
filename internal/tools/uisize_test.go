package tools

// Test Traceability Table
// | ID    | Category | Description                                                                 | Priority |
// | US-01 | Happy    | width and height take a number of cells or "full", and are written back so  | P0       |
// | US-02 | Error    | A size that is neither a number of cells nor "full" is refused             | P1       |
// | US-03 | Error    | A min over its max, or a negative bound, is refused with the node's path   | P1       |
// | US-04 | Happy    | A script sets and reads width "full" and the least and most sizes           | P1       |

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUISizes(t *testing.T) {
	t.Run("US-01 width and height take a number of cells or \"full\", and are written back so", func(t *testing.T) {
		tree, err := ParseUITree(json.RawMessage(`{"type":"textarea","id":"b","width":"full","height":12,"min_width":20,"max_height":30}`))
		if err != nil {
			t.Fatal(err)
		}

		encoded, _ := json.Marshal(tree)

		if !tree.Width.Full() || tree.Height.Cells() != 12 || tree.MinWidth != 20 || tree.MaxHeight != 30 {
			t.Errorf("tree = %+v", tree)
		}
		if !strings.Contains(string(encoded), `"width":"full"`) || !strings.Contains(string(encoded), `"height":12`) {
			t.Errorf("encoded = %s", encoded)
		}
	})

	t.Run("US-02 a size that is neither a number of cells nor \"full\" is refused", func(t *testing.T) {
		for _, size := range []string{`"wide"`, `-3`, `2.5`} {
			if _, err := ParseUITree(json.RawMessage(`{"type":"label","text":"x","width":` + size + `}`)); err == nil {
				t.Errorf("width %s taken", size)
			}
		}
	})

	t.Run("US-03 a min over its max, or a negative bound, is refused with the node's path", func(t *testing.T) {
		over := UINode{Type: UIPanel, Children: []UINode{{Type: UILabel, MinWidth: 30, MaxWidth: 10}}}
		negative := UINode{Type: UILabel, MinHeight: -1}

		overErr, negativeErr := validateUI(over), validateUI(negative)

		if overErr == nil || !strings.Contains(overErr.Error(), "tree.children[0] (label): min_width 30 is over max_width 10") {
			t.Errorf("over: %v", overErr)
		}
		if negativeErr == nil || !strings.Contains(negativeErr.Error(), "numbers of cells") {
			t.Errorf("negative: %v", negativeErr)
		}
	})

	t.Run("US-04 a script sets and reads width \"full\" and the least and most sizes", func(t *testing.T) {
		node := UINode{Type: UILabel, ID: "x"}

		err := applyProps(&node, map[string]any{"width": "full", "min_height": int64(3), "max_width": int64(40)})
		bad := applyProps(&node, map[string]any{"height": "tall"})

		if err != nil || !node.Width.Full() || node.MinHeight != 3 || node.MaxWidth != 40 || sizeValue(node.Width) != "full" {
			t.Errorf("err = %v node = %+v", err, node)
		}
		if bad == nil || !strings.Contains(bad.Error(), `use a number of cells or "full"`) {
			t.Errorf("bad = %v", bad)
		}
	})
}
