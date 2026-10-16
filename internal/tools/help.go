package tools

import (
	"fmt"
	"slices"
	"strings"
)

// helpTopic is one thing a tool's help explains: an action, or the view.
type helpTopic struct {
	name, text string
}

// helpAbouts are the topics that are no action, marked as topics where
// help lists them beside the actions.
var helpAbouts = []string{"view", "conflicts", "nodes", "events", "script", "example"}

// viewHelp says how to see a part of a tool's text output, in help's view
// topic; ViewGuide says it once in the system prompt, so no tool's
// description repeats it.
const viewHelp = "offset: lines to skip, from the end when negative (-50: the last 50); limit: lines (2000); " +
	"match: only the lines matching a regexp; numbered: line numbers; col and width: the bytes of each line to show, for long lines (0, 2000). " +
	"What is left out is said, with the offset or col that shows it."

const ViewGuide = "A tool with offset and limit fields shows a part of its text output. " + viewHelp

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
			name := each.name
			if slices.Contains(helpAbouts, name) {
				name = "topic " + name
			}
			if long {
				b.WriteString(name + ":\n" + each.text)
			} else {
				b.WriteString(name + ": " + each.text)
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
