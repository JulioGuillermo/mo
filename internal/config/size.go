package config

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	bytes  int64
}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1}}

// ParseSize reads a size as "10MB", "512KB", "1.5GB" or bytes, in any case;
// 0 is no limit.
func ParseSize(text string) (int64, error) {
	text = strings.ToUpper(strings.TrimSpace(text))
	if text == "" {
		return 0, nil
	}
	unit := int64(1)
	for _, each := range sizeUnits {
		if number, ok := strings.CutSuffix(text, each.suffix); ok {
			text, unit = strings.TrimSpace(number), each.bytes
			break
		}
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("use a size as 10MB, 512KB or 1GB, or 0 for no limit")
	}
	return int64(value * float64(unit)), nil
}
