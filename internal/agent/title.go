package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jg/mo/internal/llm"
)

const (
	titleTimeout   = time.Minute
	titleMaxTokens = 2048
	// titleDigestLength bounds what the model reads to name the conversation.
	titleDigestLength = 6000
	titleSystem       = "You name conversations between a user and a coding agent. Give a title of 3 to 7 words " +
		"saying what the conversation is about, in the language the user writes in. " +
		"End your answer with the title inside <title></title>, without quotes or a final period."
)

// ErrNothingToTitle is returned when there is no prompt to name the
// conversation after.
var ErrNothingToTitle = errors.New("nothing to title yet")

// SuggestTitle asks the model, apart from the conversation, for a title for
// it, from the user's prompts and the last answer. onEvent, when set, gets
// what the reply streams, to say how far it is.
func (a *Agent) SuggestTitle(ctx context.Context, onEvent func(llm.Event)) (string, error) {
	prompts := a.Prompts()
	if len(prompts) == 0 {
		return "", ErrNothingToTitle
	}
	var digest strings.Builder
	for _, prompt := range prompts {
		digest.WriteString("User: " + prompt.Text + "\n\n")
	}
	for i := len(a.History) - 1; i >= 0; i-- {
		if message := a.History[i]; message.Role == llm.RoleAssistant && strings.TrimSpace(message.Text) != "" {
			digest.WriteString("Agent, last: " + message.Text + "\n")
			break
		}
	}
	text := digest.String()
	if runes := []rune(text); len(runes) > titleDigestLength {
		text = string(runes[:titleDigestLength/2]) + "\n…\n" + string(runes[len(runes)-titleDigestLength/2:])
	}
	ctx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()
	response, err := a.Provider.Stream(ctx, llm.Request{
		Model:     a.Model,
		System:    titleSystem,
		MaxTokens: titleMaxTokens,
		Messages:  []llm.Message{{Role: llm.RoleUser, Text: "Name this conversation:\n\n" + text}},
	}, func(event llm.Event) {
		if onEvent != nil {
			onEvent(event)
		}
	})
	if err != nil {
		return "", err
	}
	title := readTitle(response.Message.Text)
	if title == "" {
		return "", errors.New("the model gave no title")
	}
	return title, nil
}

var titleTag = regexp.MustCompile(`(?is)<title>(.*?)</title>`)

// readTitle takes the title from the last <title></title> of an answer, so
// what the model says before it is left out; without the tags, from its
// last line.
func readTitle(text string) string {
	if found := titleTag.FindAllStringSubmatch(text, -1); len(found) > 0 {
		return cleanTitle(found[len(found)-1][1])
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return cleanTitle(lines[len(lines)-1])
}

// cleanTitle keeps the first line of a title, without the markup and
// quotes a model may put around it.
func cleanTitle(text string) string {
	const markup = "#*_`\"'“”«» "
	for _, line := range strings.Split(text, "\n") {
		line = strings.Trim(line, markup)
		for _, label := range []string{"title:", "título:", "titulo:"} {
			if strings.HasPrefix(strings.ToLower(line), label) {
				line = line[len(label):]
			}
		}
		line = strings.TrimSuffix(strings.Trim(line, markup), ".")
		line = strings.Trim(line, markup)
		if line != "" {
			return line
		}
	}
	return ""
}
