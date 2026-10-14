package tools

import (
	"fmt"
	"strings"
)

// helpTopic is one thing a tool's help explains: an action, or the view.
type helpTopic struct {
	name, text string
}

const viewHelp = "offset: lines to skip, from the end when negative (-50: the last 50); limit: lines to show (2000); " +
	"col: byte of each line to start at (0); width: bytes of each line (2000); match: only the lines matching a regexp; " +
	"numbered: line numbers. What is left out is said, with the offset or col that shows it."

// help answers a tool's help action: every topic in a line, or one whole.
func help(topics []helpTopic, topic string) (string, error) {
	topics = append(topics, helpTopic{"view", viewHelp})
	if topic == "" {
		var lines []string
		for _, each := range topics {
			lines = append(lines, each.name+": "+each.text)
		}
		return strings.Join(lines, "\n"), nil
	}
	var names []string
	for _, each := range topics {
		if each.name == topic {
			return each.text, nil
		}
		names = append(names, each.name)
	}
	return "", fmt.Errorf("no help on %q: %s", topic, strings.Join(names, ", "))
}
