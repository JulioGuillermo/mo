package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AskName is the tool the model asks the user questions with. The agent
// answers it, since only the user can.
const AskName = "ask"

const (
	mostQuestions = 4
	mostOptions   = 6
)

// Question is one question of an ask call: free text, or choices the user
// picks one of, or several of, and can always answer in their own words.
type Question struct {
	Text     string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
}

func askTool() Tool {
	return Tool{
		Name:        AskName,
		Description: "Ask the user when you need a decision or information you cannot find yourself. Up to 4 questions; options: short choices; multiple: several may be picked. The user can always answer in their own words.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"questions":{"type":"array","items":{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}},"multiple":{"type":"boolean"}},"required":["question"]}}},"required":["questions"]}`),
		Run: func(context.Context, Env, json.RawMessage) (string, error) {
			return "", errors.New("ask is answered by the user through the agent")
		},
		Summary: func(args json.RawMessage) string {
			questions, err := ParseQuestions(args)
			if err != nil {
				return err.Error()
			}
			return questions[0].Text
		},
	}
}

// ParseQuestions reads and checks the questions of an ask call.
func ParseQuestions(raw json.RawMessage) ([]Question, error) {
	var args struct {
		Questions []Question `json:"questions"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Questions) == 0 || len(args.Questions) > mostQuestions {
		return nil, fmt.Errorf("ask: send 1 to %d questions", mostQuestions)
	}
	for i, question := range args.Questions {
		args.Questions[i].Text = strings.TrimSpace(question.Text)
		if args.Questions[i].Text == "" {
			return nil, fmt.Errorf("ask: question %d is empty", i+1)
		}
		if len(question.Options) > mostOptions {
			return nil, fmt.Errorf("ask: question %d has more than %d options", i+1, mostOptions)
		}
		var options []string
		for _, option := range question.Options {
			if option = strings.TrimSpace(option); option != "" {
				options = append(options, option)
			}
		}
		args.Questions[i].Options = options
	}
	return args.Questions, nil
}
