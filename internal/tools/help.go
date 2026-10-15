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

// help answers a tool's help action, the view explained too: every topic
// in a line, or one whole.
func help(topics []helpTopic, topic string) (string, error) {
	return helpOn(append(topics, helpTopic{"view", viewHelp}), topic)
}

// helpOn answers a help action on just the topics given; a topic of
// several lines starts on its own, set apart.
func helpOn(topics []helpTopic, topic string) (string, error) {
	if topic == "" {
		var b strings.Builder
		apart := false
		for i, each := range topics {
			long := strings.Contains(each.text, "\n")
			if i > 0 {
				b.WriteString("\n")
				if long || apart {
					b.WriteString("\n")
				}
			}
			if long {
				b.WriteString(each.name + ":\n" + each.text)
			} else {
				b.WriteString(each.name + ": " + each.text)
			}
			apart = long
		}
		return b.String(), nil
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
