package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/permissions"
)

const (
	judgeTimeout        = 60 * time.Second
	judgeMaxTokens      = 1024
	judgedRequestLength = 2000
	judgeSystem         = `You check shell commands and web fetches that an AI coding agent wants to run on the user's computer, before they run.
Answer SAFE when every one only reads, lists, searches, builds, tests, or changes files inside the working directory as the user's request needs.
Answer UNSAFE when any one could delete or overwrite data outside the working directory, install software, download and run code, change system or user settings, use credentials, send data from the computer to anywhere, or does something the request does not need.
When unsure, answer UNSAFE.
Reply with SAFE or UNSAFE, then a reason of a few words.`
)

// approval is whether a call needs the user's answer, and what to show.
type approval struct {
	ask      bool
	warning  string
	reason   string
	commands []string
	ruled    bool
}

// approvalFor decides whether a call needs the user. A deny rule always
// asks; the mode all asks nothing else; a warning asks in every other mode.
// Shell commands and fetches pass when rules allow every part of them, or
// in auto mode when the model judges them safe. In ask mode, file changes
// ask too.
func (a *Agent) approvalFor(ctx context.Context, call llm.ToolCall, hooks Hooks) approval {
	warning := a.Tools.Warning(call)
	listed := slices.Contains(a.ConfirmTools, call.Name) && !a.alwaysAllowed[call.Name] && !a.Tools.AlreadyConfirmed(call)
	policy := a.Permissions
	if policy == nil {
		return approval{ask: warning != "" || listed, warning: warning}
	}
	subjects, ruled, splitErr := ruledSubjects(call)
	for _, subject := range subjects {
		if verdict, rule := policy.Check(subject.Text); verdict == permissions.Denied {
			return approval{ask: true, warning: "deny rule: " + rule, ruled: ruled}
		}
	}
	mode := policy.Mode()
	switch {
	case mode == permissions.ModeAll:
		return approval{}
	case warning != "":
		return approval{ask: true, warning: warning, ruled: ruled}
	case !ruled:
		changes := mode == permissions.ModeAsk && a.Tools.Changes(call) && !a.alwaysAllowed[call.Name]
		return approval{ask: listed || changes}
	case !slices.Contains(a.ConfirmTools, call.Name) || a.Tools.AlreadyConfirmed(call):
		return approval{}
	case splitErr != nil:
		return approval{ask: true, ruled: true, reason: "the command could not be split to check its parts"}
	}
	var unknown []string
	for _, subject := range subjects {
		if verdict, _ := policy.Check(subject.Text); verdict != permissions.Allowed || subject.Writes {
			unknown = append(unknown, subject.Text)
		}
	}
	if len(unknown) == 0 {
		return approval{}
	}
	reason := ""
	if mode == permissions.ModeAuto {
		hooks.activity("checking whether " + firstOf(unknown) + " is safe")
		safe, why := a.judge(ctx, unknown)
		hooks.activity("")
		if safe {
			if hooks.OnNotice != nil {
				hooks.OnNotice(fmt.Sprintf("auto: %s · %s", firstOf(unknown), why))
			}
			return approval{}
		}
		reason = "auto: " + why
	}
	return approval{ask: true, ruled: true, commands: unknown, reason: reason}
}

// ruledSubjects returns what rules are checked against: the simple commands
// of a bash script, or "web_fetch <url>". ruled is false for other tools.
func ruledSubjects(call llm.ToolCall) (subjects []permissions.Command, ruled bool, err error) {
	switch call.Name {
	case "bash":
		switch action := argText(call, "action"); {
		case action == "kill":
			var job struct {
				ID int `json:"id"`
			}
			_ = json.Unmarshal(call.Args, &job)
			return []permissions.Command{{Text: fmt.Sprintf("kill job %d", job.ID), Writes: true}}, true, nil
		case action != "" && action != "run", argText(call, "command") == "":
			return nil, false, nil
		}
		subjects, err = permissions.Commands(argText(call, "command"))
		return subjects, true, err
	case "web_fetch", "fetch":
		return []permissions.Command{{Text: "web_fetch " + argText(call, "url")}}, true, nil
	}
	return nil, false, nil
}

// judge asks the model whether commands are safe for the user's request.
// Only the request and the commands are shown, not what tools returned, so
// a page or file cannot talk it into an answer. Safe answers are kept for
// the session.
func (a *Agent) judge(ctx context.Context, commands []string) (bool, string) {
	key := strings.Join(commands, "\n")
	if a.judgedSafe[key] {
		return true, "judged safe before"
	}
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	request := a.currentRequest
	if runes := []rune(request); len(runes) > judgedRequestLength {
		request = string(runes[:judgedRequestLength]) + "…"
	}
	response, err := a.Provider.Stream(ctx, llm.Request{
		Model:     a.Model,
		System:    judgeSystem,
		MaxTokens: judgeMaxTokens,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: fmt.Sprintf(
			"Working directory: %s\nThe user asked: %s\n\nTo run:\n%s", a.Tools.WorkDir(), request, key)}},
	}, func(llm.Event) {})
	if err != nil {
		return false, "the model could not judge it: " + err.Error()
	}
	safe, reason := readJudgement(response.Message.Text)
	if safe {
		if a.judgedSafe == nil {
			a.judgedSafe = map[string]bool{}
		}
		a.judgedSafe[key] = true
	}
	return safe, reason
}

// readJudgement reads SAFE or UNSAFE first; anything else counts as unsafe.
func readJudgement(text string) (bool, string) {
	text = strings.TrimSpace(text)
	word, reason, _ := strings.Cut(text, " ")
	word = strings.ToUpper(strings.Trim(word, "*_.:,`\"'"))
	reason = strings.TrimLeft(strings.TrimSpace(reason), "-—:,. ")
	reason = strings.SplitN(reason, "\n", 2)[0]
	switch word {
	case "SAFE":
		return true, orElse(reason, "judged safe")
	case "UNSAFE":
		return false, orElse(reason, "judged unsafe")
	}
	return false, "no clear answer from the model"
}

func orElse(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

func firstOf(commands []string) string {
	if len(commands) == 1 {
		return commands[0]
	}
	return fmt.Sprintf("%s (+%d)", commands[0], len(commands)-1)
}

func argText(call llm.ToolCall, name string) string {
	var args map[string]any
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return ""
	}
	text, _ := args[name].(string)
	return text
}
