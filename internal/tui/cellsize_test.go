package tui

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | CZ-01 | Happy    | Settings › Look and keys › Cell size saves WIDTHxHEIGHT, normalized      | P1       |
// | CZ-02 | Happy    | auto, the default, is not written to the config                          | P1       |
// | CZ-03 | Error    | A wrong size is refused on save and in the field                         | P1       |
// | CZ-04 | Happy    | The config's size is put in force; auto asks the terminal again          | P0       |

import (
	"testing"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/render"
)

func TestCellSizeSetting(t *testing.T) {
	base := config.Config{Provider: "llama-server"}

	t.Run("CZ-01 Settings › Look and keys › Cell size saves WIDTHxHEIGHT, normalized", func(t *testing.T) {
		draft := newSettingsDraft(base)
		draft.cellSize = " 10 X 20 "

		layer, err := draft.applyTo(base, nil)

		if layer.Render.CellSize != "10x20" || err != nil {
			t.Errorf("got %q, %v", layer.Render.CellSize, err)
		}
	})

	t.Run("CZ-02 auto, the default, is not written to the config", func(t *testing.T) {
		draft := newSettingsDraft(base)
		untouched, _ := draft.applyTo(base, nil)
		draft.cellSize = "auto"
		typed, err := draft.applyTo(base, nil)

		if untouched.Render.CellSize != "" || typed.Render.CellSize != "" || err != nil || config.Defaults().Render.CellSize != render.CellSizeAuto {
			t.Errorf("untouched = %q typed = %q err = %v", untouched.Render.CellSize, typed.Render.CellSize, err)
		}
	})

	t.Run("CZ-03 A wrong size is refused on save and in the field", func(t *testing.T) {
		draft := newSettingsDraft(base)
		draft.cellSize = "8 by 16"

		_, err := draft.applyTo(base, nil)

		if err == nil || validCellSize("8 by 16") == nil || validCellSize("8x16") != nil {
			t.Errorf("save err = %v", err)
		}
	})

	t.Run("CZ-04 The config's size is put in force; auto asks the terminal again", func(t *testing.T) {
		defer render.SetCellSize(0, 0)
		useCellSize("12x25")
		setWidth, setHeight := render.CellSize()
		useCellSize("auto")
		autoWidth, autoHeight := render.CellSize()

		if setWidth != 12 || setHeight != 25 || (autoWidth == 12 && autoHeight == 25) {
			t.Errorf("set = %dx%d auto = %dx%d", setWidth, setHeight, autoWidth, autoHeight)
		}
	})
}
