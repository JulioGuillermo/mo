package agent

// Test Traceability Table
// | ID    | Category | Description                                                           | Priority |
// | TI-01 | Happy    | The model names the conversation from the prompts and the last answer | P0       |
// | TI-02 | Edge     | Quotes, markup, a "Title:" label and the final period are taken off   | P1       |
// | TI-03 | Error    | Without prompts there is nothing to title, and the model is not asked | P1       |

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jg/mo/internal/llm"
)

func TestSuggestTitle(t *testing.T) {
	t.Run("TI-01 the model names the conversation from the prompts and the last answer", func(t *testing.T) {
		provider := &scriptedProvider{replies: []llm.Message{{Role: llm.RoleAssistant, Text: "Arreglar el login con OAuth"}}}
		a := newAgent(t, provider)
		a.History = []llm.Message{
			{Role: llm.RoleUser, Text: "el login falla"},
			{Role: llm.RoleAssistant, Text: "Es el token de OAuth"},
			{Role: llm.RoleUser, Text: "arréglalo"},
		}

		title, err := a.SuggestTitle(context.Background())

		sent := provider.requests[0].Messages[0].Text
		if err != nil || title != "Arreglar el login con OAuth" || len(a.History) != 3 {
			t.Errorf("title %q err %v", title, err)
		}
		if !strings.Contains(sent, "el login falla") || !strings.Contains(sent, "arréglalo") || !strings.Contains(sent, "token de OAuth") || provider.requests[0].Tools != nil {
			t.Errorf("sent:\n%s", sent)
		}
	})

	t.Run("TI-02 quotes, markup, a Title: label and the final period are taken off", func(t *testing.T) {
		for answer, want := range map[string]string{
			"\"Fix the login.\"":           "Fix the login",
			"**Title:** Fix the login\nok": "Fix the login",
			"\n# «Arreglar el login»\n":    "Arreglar el login",
		} {
			if got := cleanTitle(answer); got != want {
				t.Errorf("%q → %q, want %q", answer, got, want)
			}
		}
	})

	t.Run("TI-03 without prompts there is nothing to title, and the model is not asked", func(t *testing.T) {
		provider := &scriptedProvider{}
		a := newAgent(t, provider)

		_, err := a.SuggestTitle(context.Background())

		if !errors.Is(err, ErrNothingToTitle) || len(provider.requests) != 0 {
			t.Errorf("err %v requests %d", err, len(provider.requests))
		}
	})
}
