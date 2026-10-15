package tools

import (
	"fmt"
	"math"
	"slices"
	"strconv"
)

// HasValue tells a node the user sets a value of: an input, a select, a
// slider, a checkbox, a switch or a radio.
func (n UINode) HasValue() bool {
	switch n.Type {
	case UIInput, UISelect, UISlider, UICheckbox, UISwitch, UIRadio:
		return true
	}
	return false
}

// Focusable tells a node the user can tab to.
func (n UINode) Focusable() bool {
	return n.Type == UIButton || n.HasValue()
}

// Range is a slider's least, greatest and step: 0 to 100 by 1 unless said.
func (n UINode) Range() (least, most, step float64) {
	least, most, step = n.Min, n.Max, n.Step
	if most <= least {
		most = least + 100
	}
	if step <= 0 {
		step = 1
	}
	return least, most, step
}

// Current is the node's value, or what it starts as: a select's first
// option, a slider's least, a checkbox or switch off.
func (n UINode) Current() string {
	if n.Value != "" {
		return n.Value
	}
	switch n.Type {
	case UISelect:
		if len(n.Options) > 0 {
			return n.Options[0]
		}
	case UISlider:
		least, _, _ := n.Range()
		return FormatNumber(least)
	case UICheckbox, UISwitch:
		return "false"
	}
	return ""
}

// Number is a slider's value, within its range.
func (n UINode) Number() float64 {
	least, most, _ := n.Range()
	value, err := strconv.ParseFloat(n.Current(), 64)
	if err != nil {
		return least
	}
	return math.Min(math.Max(value, least), most)
}

// Checked tells whether a checkbox or switch is on.
func (n UINode) Checked() bool {
	return n.Current() == "true"
}

// Moved is the value a select, radio or slider takes moving by steps: the
// next or earlier option, wrapping in a select, or the slider by its step.
func (n UINode) Moved(by int) string {
	switch n.Type {
	case UISelect, UIRadio:
		if len(n.Options) == 0 {
			return ""
		}
		at := slices.Index(n.Options, n.Current())
		if at < 0 {
			if by < 0 {
				return n.Options[len(n.Options)-1]
			}
			return n.Options[0]
		}
		next := at + by
		if n.Type == UISelect {
			next = ((next % len(n.Options)) + len(n.Options)) % len(n.Options)
		} else {
			next = min(max(next, 0), len(n.Options)-1)
		}
		return n.Options[next]
	case UISlider:
		_, _, step := n.Range()
		return n.AtNumber(n.Number() + float64(by)*step)
	}
	return n.Current()
}

// AtNumber is a slider's value at a number, on a step and within range.
func (n UINode) AtNumber(value float64) string {
	least, most, step := n.Range()
	value = least + math.Round((value-least)/step)*step
	return FormatNumber(math.Min(math.Max(value, least), most))
}

// AtFraction is a slider's value a fraction of the way along it.
func (n UINode) AtFraction(fraction float64) string {
	least, most, _ := n.Range()
	return n.AtNumber(least + math.Min(math.Max(fraction, 0), 1)*(most-least))
}

// Toggled is a checkbox's or switch's value flipped.
func (n UINode) Toggled() string {
	return strconv.FormatBool(!n.Checked())
}

// FormatNumber is a number as a slider shows and keeps it.
func FormatNumber(value float64) string {
	return strconv.FormatFloat(math.Round(value*1e6)/1e6, 'f', -1, 64)
}

// checkValue says what is wrong with a control's options, range or value.
func checkValue(n UINode, where string) error {
	switch n.Type {
	case UISelect, UIRadio:
		if len(n.Options) == 0 {
			return fmt.Errorf(`%s needs options, e.g. "options":["small","medium","large"]`, where)
		}
		if n.Value != "" && !slices.Contains(n.Options, n.Value) {
			return fmt.Errorf("%s: value %q is none of its options", where, n.Value)
		}
	case UISlider:
		if n.Max != 0 && n.Max <= n.Min {
			return fmt.Errorf("%s: max %v is not above min %v", where, n.Max, n.Min)
		}
		if n.Value != "" {
			if _, err := strconv.ParseFloat(n.Value, 64); err != nil {
				return fmt.Errorf("%s: value %q is not a number", where, n.Value)
			}
		}
	case UICheckbox, UISwitch:
		if n.Value != "" && n.Value != "true" && n.Value != "false" {
			return fmt.Errorf("%s: value %q: use true or false", where, n.Value)
		}
	}
	return nil
}
