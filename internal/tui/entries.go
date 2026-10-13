package tui

import (
	"errors"
	"fmt"
	"maps"
	"strings"
)

// withEntry returns a copy of entries with name set to value.
func withEntry[V any](entries map[string]V, name string, value V) map[string]V {
	entries = maps.Clone(entries)
	if entries == nil {
		entries = map[string]V{}
	}
	entries[name] = value
	return entries
}

func withoutEntry[V any](entries map[string]V, name string) map[string]V {
	entries = maps.Clone(entries)
	delete(entries, name)
	return entries
}

// validNewName accepts a short name that no entry has yet.
func validNewName[V any](entries map[string]V) func(string) error {
	return func(name string) error {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " .\"'") {
			return errors.New("use a short name without spaces or dots")
		}
		if _, exists := entries[name]; exists {
			return fmt.Errorf("%s already exists; pick it in the list", name)
		}
		return nil
	}
}
