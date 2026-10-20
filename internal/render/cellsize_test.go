package render

// Test Traceability Table
// | ID    | Category | Description                                                        | Priority |
// | CS-01 | Happy    | ParseCellSize reads WIDTHxHEIGHT, spaces and capitals aside         | P1       |
// | CS-02 | Happy    | auto or nothing parses as 0, 0                                      | P1       |
// | CS-03 | Error    | Anything else, zero, negative or too big is refused                 | P1       |
// | CS-04 | Happy    | A set size wins; 0 goes back to the terminal or the 8x16 guess      | P0       |

import "testing"

func TestCellSize(t *testing.T) {
	t.Run("CS-01 ParseCellSize reads WIDTHxHEIGHT, spaces and capitals aside", func(t *testing.T) {
		width, height, err := ParseCellSize(" 9 X 18 ")

		if width != 9 || height != 18 || err != nil {
			t.Errorf("got %d x %d, %v", width, height, err)
		}
	})

	t.Run("CS-02 auto or nothing parses as 0, 0", func(t *testing.T) {
		for _, text := range []string{"", "auto", "AUTO"} {
			width, height, err := ParseCellSize(text)
			if width != 0 || height != 0 || err != nil {
				t.Errorf("%q: got %d x %d, %v", text, width, height, err)
			}
		}
	})

	t.Run("CS-03 Anything else, zero, negative or too big is refused", func(t *testing.T) {
		for _, text := range []string{"8", "8x", "x16", "ax16", "0x16", "8x-1", "8x201", "8x16x2"} {
			if _, _, err := ParseCellSize(text); err == nil {
				t.Errorf("%q was taken", text)
			}
		}
	})

	t.Run("CS-04 A set size wins; 0 goes back to the terminal or the 8x16 guess", func(t *testing.T) {
		defer SetCellSize(0, 0)
		SetCellSize(11, 23)
		setWidth, setHeight := CellSize()
		SetCellSize(0, 0)
		autoWidth, autoHeight := CellSize()
		terminalWidth, terminalHeight, reported := terminalCellSize()

		if setWidth != 11 || setHeight != 23 {
			t.Errorf("set: got %d x %d", setWidth, setHeight)
		}
		if reported && (autoWidth != terminalWidth || autoHeight != terminalHeight) {
			t.Errorf("auto: got %d x %d, terminal says %d x %d", autoWidth, autoHeight, terminalWidth, terminalHeight)
		}
		if !reported && (autoWidth != fallbackCellWidth || autoHeight != fallbackCellHeight) {
			t.Errorf("auto: got %d x %d, want the guess", autoWidth, autoHeight)
		}
	})
}
