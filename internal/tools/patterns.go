package tools

import "path"

// Matches tells whether name is one of patterns, where * stands for any
// characters, as mcp__github__* for all the tools of a server.
func Matches(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	return false
}
