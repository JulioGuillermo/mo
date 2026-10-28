package tui

// Test Traceability Table
// | ID    | Category | Description                                                            | Priority |
// | US-01 | Happy    | Each limit is a bar with its share and when it resets, names lined up  | P0       |
// | US-02 | Edge     | A provider without limits, or failing, says so on its line             | P1       |
// | US-03 | Happy    | The reset reads as minutes, hours or days from now, with the time      | P1       |
// | US-04 | Happy    | Only sign-ins to Claude, ChatGPT or Antigravity plans are asked        | P0       |

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

func TestUsage(t *testing.T) {
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.Local)

	t.Run("US-01 each limit is a bar with its share and when it resets, names lined up", func(t *testing.T) {
		found := []providerUsage{{provider: "claude", quotas: []llm.Quota{
			{Name: "5h", Used: 0.5, Resets: now.Add(90 * time.Minute)},
			{Name: "week", Used: 0.1},
		}}}

		got := ansi.Strip(drawUsage(found, statusPalette(true), "blocks", now))

		want := []string{
			"◇ usage of the plans",
			"  claude",
			"    5h    " + ansi.Strip(contextBar(0.5, usageBarCells, nil, nil, "blocks")) + "  50%  resets in 1h 30m · 10:30",
			"    week  " + ansi.Strip(contextBar(0.1, usageBarCells, nil, nil, "blocks")) + "  10%",
		}
		if got != strings.Join(want, "\n") {
			t.Errorf("got\n%s\nwant\n%s", got, strings.Join(want, "\n"))
		}
	})

	t.Run("US-02 a provider without limits, or failing, says so on its line", func(t *testing.T) {
		found := []providerUsage{
			{provider: "gemini", none: "no plan limits to ask about: paid by use, or local"},
			{provider: "codex", err: errors.New("401 Unauthorized: expired\nmore")},
		}

		got := ansi.Strip(drawUsage(found, statusPalette(true), "", now))

		if !strings.Contains(got, "  gemini  no plan limits") || !strings.Contains(got, "  codex  401 Unauthorized: expired …") {
			t.Errorf("got\n%s", got)
		}
	})

	t.Run("US-03 the reset reads as minutes, hours or days from now, with the time", func(t *testing.T) {
		cases := map[time.Duration]string{
			-time.Minute:           "resets now",
			23 * time.Minute:       "resets in 23m · 09:23",
			(6*24 + 4) * time.Hour: "resets in 6d 4h · Tue 3 Nov 13:00",
		}
		for left, want := range cases {
			if got := resetsIn(now.Add(left), now); got != want {
				t.Errorf("%v: got %q, want %q", left, got, want)
			}
		}
	})

	t.Run("US-04 only sign-ins to Claude, ChatGPT or Antigravity plans are asked", func(t *testing.T) {
		cases := []struct {
			settings config.ProviderConfig
			want     bool
		}{
			{config.ProviderConfig{Protocol: "claude-code", TokenFrom: "claude-code"}, true},
			{config.ProviderConfig{Protocol: "responses", OAuth: &config.OAuth{}}, true},
			{config.ProviderConfig{Protocol: "cloudcode", OAuth: &config.OAuth{}}, true},
			{config.ProviderConfig{Protocol: "anthropic", APIKey: "k"}, false},
			{config.ProviderConfig{Protocol: "openai", OAuth: &config.OAuth{}}, false},
		}
		for _, c := range cases {
			if hasPlan(c.settings) != c.want {
				t.Errorf("%+v: want %v", c.settings.Protocol, c.want)
			}
		}
	})
}
