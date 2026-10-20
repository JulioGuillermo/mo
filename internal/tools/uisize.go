package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// UISize is a node's width or height: a number of cells, 0 for as much as
// it needs, or UIFull for all the room its parent gives it.
type UISize int

// UIFull is the size of a node that takes all the room its parent gives:
// in a row or a column, what the others leave along it; at the root, the
// whole dialog.
const UIFull UISize = -1

const uiFullName = "full"

// Full tells whether the size takes all the room there is.
func (s UISize) Full() bool { return s == UIFull }

// Cells is a fixed size in cells, 0 when it has none.
func (s UISize) Cells() int { return max(int(s), 0) }

func (s UISize) MarshalJSON() ([]byte, error) {
	if s.Full() {
		return json.Marshal(uiFullName)
	}
	return json.Marshal(int(s))
}

func (s *UISize) UnmarshalJSON(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := parseUISize(value)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// parseUISize reads a size as a tree or a script gives it: a number of
// cells, or "full".
func parseUISize(value any) (UISize, error) {
	switch value := value.(type) {
	case nil:
		return 0, nil
	case string:
		if strings.EqualFold(strings.TrimSpace(value), uiFullName) {
			return UIFull, nil
		}
	case float64:
		if value >= 0 && value == math.Trunc(value) {
			return UISize(value), nil
		}
	case int64:
		if value >= 0 {
			return UISize(value), nil
		}
	}
	return 0, fmt.Errorf(`%v: use a number of cells or "full"`, value)
}

// checkSizes says what is wrong with a node's least and most sizes.
func checkSizes(n UINode, where string) error {
	for name, pair := range map[string][2]int{"width": {n.MinWidth, n.MaxWidth}, "height": {n.MinHeight, n.MaxHeight}} {
		least, most := pair[0], pair[1]
		if least < 0 || most < 0 {
			return fmt.Errorf("%s: min_%s and max_%s are numbers of cells", where, name, name)
		}
		if most > 0 && least > most {
			return fmt.Errorf("%s: min_%s %d is over max_%s %d", where, name, least, name, most)
		}
	}
	return nil
}
