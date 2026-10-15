package tools

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// uiNodeKeys are the fields a node takes, as UINode names them.
var uiNodeKeys = []string{
	"type", "id", "text", "label", "value", "placeholder", "options", "min", "max", "step", "title", "direction", "columns", "widths", "justify", "align", "gap", "grow",
	"width", "height", "padding", "margin", "bg", "fg", "border", "bold", "visible", "disabled", "on_click", "on_input",
	"on_change", "children",
}

// uiKeyAliases are the names small models give the fields, taken as the
// fields they mean.
var uiKeyAliases = map[string]string{
	"kind": "type", "element": "type", "node": "type", "component": "type", "widget": "type",
	"content": "text", "caption": "label", "name": "id", "key": "id",
	"onclick": "on_click", "onClick": "on_click", "click": "on_click", "action": "on_click",
	"oninput": "on_input", "onInput": "on_input", "onchange": "on_change", "onChange": "on_change",
	"items": "children", "elements": "children", "nodes": "children", "content_items": "children",
	"choices": "options", "values": "options", "checked": "value", "selected": "value", "default": "value",
	"cols": "columns", "column_count": "columns", "column_widths": "widths", "cells": "children",
	"minimum": "min", "maximum": "max", "increment": "step",
	"color": "fg", "foreground": "fg", "background": "bg", "flex": "grow", "layout": "direction",
}

// uiTypeAliases are the types small models reach for, taken as the ones
// there are.
var uiTypeAliases = map[string]string{
	"row": UIPanel, "column": UIPanel, "col": UIPanel, "box": UIPanel, "container": UIPanel, "div": UIPanel,
	"stack": UIPanel, "vbox": UIPanel, "hbox": UIPanel, "form": UIPanel, "view": UIPanel, "group": UIPanel,
	"table": UIGrid, "form_grid": UIGrid,
	"section": UICard, "frame": UICard, "window": UICard, "dialog": UICard,
	"text": UILabel, "span": UILabel, "p": UILabel, "heading": UILabel, "title": UILabel, "h1": UILabel, "h2": UILabel,
	"dropdown": UISelect, "combo": UISelect, "combobox": UISelect, "picker": UISelect, "choice": UISelect, "menu": UISelect,
	"range": UISlider, "scale": UISlider, "check": UICheckbox, "check_box": UICheckbox, "tick": UICheckbox,
	"toggle": UISwitch, "radio_group": UIRadio, "radiogroup": UIRadio, "radios": UIRadio, "radiobutton": UIRadio, "radio_button": UIRadio,
	"btn": UIButton, "textbox": UIInput, "textfield": UIInput, "text_input": UIInput, "field": UIInput, "entry": UIInput,
}

// uiTreeExample is a whole tree, shown wherever a tree is got wrong.
const uiTreeExample = `{"type":"card","title":"Login","children":[{"type":"label","text":"Your name"},` +
	`{"type":"input","id":"name","placeholder":"name"},{"type":"button","id":"ok","label":"OK","on_click":"submit"}]}`

// parseUITree reads the tree a call gave, mending what small models get
// wrong when what they meant is plain: a tree sent as a string, a list of
// nodes, a node under its type's name, other names for fields and types,
// and a missing type that the other fields tell.
func parseUITree(raw json.RawMessage) (*UINode, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}
	if text, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, fmt.Errorf("tree is a string that is not JSON: send it as an object, e.g. %s", uiTreeExample)
		}
	}
	mended, err := mendUINode(value, "tree")
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(mended)
	var node UINode
	if err := json.Unmarshal(encoded, &node); err != nil {
		return nil, fmt.Errorf("tree: %w; e.g. %s", err, uiTreeExample)
	}
	return &node, nil
}

func mendUINode(value any, path string) (map[string]any, error) {
	var node map[string]any
	switch v := value.(type) {
	case []any:
		node = map[string]any{"type": UIPanel, "children": v}
	case map[string]any:
		node = v
	case string:
		node = map[string]any{"type": UILabel, "text": v}
	default:
		return nil, fmt.Errorf("%s: a node is an object, e.g. %s", path, uiTreeExample)
	}
	// {"card": {...}}: a node under its type's name.
	if len(node) == 1 {
		for name, inner := range node {
			if fields, ok := inner.(map[string]any); ok && uiTypeOf(name) != "" {
				if _, typed := fields["type"]; !typed {
					node = maps.Clone(fields)
					node["type"] = uiTypeOf(name)
				}
			}
		}
	}
	// A grid as rows of cells: its children row by row, as many columns
	// as the widest row.
	if rows, ok := node["rows"].([]any); ok {
		lists := make([][]any, len(rows))
		columns := 0
		for i, row := range rows {
			each, isList := row.([]any)
			if !isList {
				each = []any{row}
			}
			lists[i] = each
			columns = max(columns, len(each))
		}
		var cells []any
		for _, each := range lists {
			cells = append(cells, each...)
			for range columns - len(each) {
				cells = append(cells, map[string]any{"type": UILabel})
			}
		}
		node = maps.Clone(node)
		delete(node, "rows")
		node["children"] = cells
		if node["columns"] == nil && node["cols"] == nil {
			node["columns"] = columns
		}
		if node["type"] == nil {
			node["type"] = UIGrid
		}
	}
	mended := map[string]any{}
	var unknown []string
	for name, field := range node {
		key := name
		if !slices.Contains(uiNodeKeys, key) {
			alias, ok := uiKeyAliases[key]
			if !ok {
				alias, ok = uiKeyAliases[strings.ToLower(key)]
			}
			if !ok {
				unknown = append(unknown, name)
				continue
			}
			if _, taken := node[alias]; taken {
				continue
			}
			key = alias
		}
		mended[key] = field
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s: unknown field %s; a node takes %s", path, strings.Join(quoteAll(unknown), ", "), strings.Join(uiNodeKeys, ", "))
	}
	if typed, ok := mended["type"].(string); ok && typed != "" {
		if known := uiTypeOf(typed); known != "" {
			if direction := uiDirectionOf(typed); direction != "" && mended["direction"] == nil {
				mended["direction"] = direction
			}
			mended["type"] = known
		}
	} else if mended["type"] == nil || mended["type"] == "" {
		guessed := guessUIType(mended)
		if guessed == "" {
			return nil, fmt.Errorf(`%s: no "type": every node needs one of %s, e.g. %s`, path, strings.Join(UITypes, ", "), uiTreeExample)
		}
		mended["type"] = guessed
	}
	if direction, ok := mended["direction"].(string); ok && uiDirectionOf(direction) != "" {
		mended["direction"] = uiDirectionOf(direction)
	}
	// A select's or radio's options sent as children or as objects.
	if kind := mended["type"]; kind == UISelect || kind == UIRadio {
		if _, has := mended["options"]; !has && mended["children"] != nil {
			mended["options"] = mended["children"]
		}
		delete(mended, "children")
	}
	if options, ok := mended["options"].([]any); ok {
		texts := make([]any, len(options))
		for i, option := range options {
			texts[i] = optionText(option)
		}
		mended["options"] = texts
	}
	if value, ok := mended["value"]; ok {
		mended["value"] = valueText(value)
	}
	// A button's words are its label, a label's its text: take either.
	switch mended["type"] {
	case UIButton, UISelect, UISlider, UICheckbox, UISwitch, UIRadio:
		if _, has := mended["label"]; !has && mended["text"] != nil {
			mended["label"] = mended["text"]
			delete(mended, "text")
		}
	case UILabel:
		if _, has := mended["text"]; !has && mended["label"] != nil {
			mended["text"] = mended["label"]
			delete(mended, "label")
		}
	}
	if children, ok := mended["children"]; ok {
		list, isList := children.([]any)
		if !isList {
			list = []any{children}
		}
		mendedChildren := make([]any, 0, len(list))
		for i, child := range list {
			each, err := mendUINode(child, fmt.Sprintf("%s.children[%d]", path, i))
			if err != nil {
				return nil, err
			}
			mendedChildren = append(mendedChildren, each)
		}
		mended["children"] = mendedChildren
	}
	return mended, nil
}

// uiTypeOf is the type a name stands for, or empty.
func uiTypeOf(name string) string {
	lower := strings.ToLower(name)
	if slices.Contains(UITypes, lower) {
		return lower
	}
	return uiTypeAliases[lower]
}

// uiDirectionOf is the direction a name stands for, col being column,
// or empty.
func uiDirectionOf(name string) string {
	switch strings.ToLower(name) {
	case "row", "horizontal", "hbox":
		return "row"
	case "column", "col", "vertical", "vbox":
		return "column"
	}
	return ""
}

// guessUIType tells a node's type from its other fields, when they do.
func guessUIType(node map[string]any) string {
	has := func(names ...string) bool {
		for _, name := range names {
			if _, ok := node[name]; ok {
				return true
			}
		}
		return false
	}
	switch {
	case has("columns", "widths"):
		return UIGrid
	case has("children"):
		if has("title") {
			return UICard
		}
		return UIPanel
	case has("options"):
		return UISelect
	case isBool(node["value"]):
		return UICheckbox
	case has("min", "max", "step"):
		return UISlider
	case has("placeholder", "value", "on_input", "on_change"):
		return UIInput
	case has("on_click", "label"):
		return UIButton
	case has("text"):
		return UILabel
	}
	return ""
}

func quoteAll(names []string) []string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return quoted
}

// optionText is an option given as an object, by its value, label or
// text.
func optionText(option any) string {
	if fields, ok := option.(map[string]any); ok {
		for _, name := range []string{"value", "label", "text", "name", "title"} {
			if value, has := fields[name]; has {
				return valueText(value)
			}
		}
	}
	return valueText(option)
}

func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}
