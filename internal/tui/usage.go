package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jg/mo/internal/auth"
	"github.com/jg/mo/internal/config"
	"github.com/jg/mo/internal/llm"
)

const (
	usageTimeout    = 15 * time.Second
	usageBarCells   = 20
	usageErrorRunes = 160
)

// planUsageMsg brings what /usage found, drawn.
type planUsageMsg string

// providerUsage is what one provider said of its plan's limits.
type providerUsage struct {
	provider string
	quotas   []llm.Quota
	// none says why the provider has no limits to ask about.
	none string
	err  error
}

// usageCommand asks the providers signed in to a plan, or the one named,
// how much of their plan's limits is used.
func (m model) usageCommand(argument string) (tea.Model, tea.Cmd) {
	var providers []string
	for _, name := range slices.Sorted(maps.Keys(m.session.Config.Providers)) {
		if hasPlan(m.session.Config.Providers[name]) {
			providers = append(providers, name)
		}
	}
	named := strings.TrimSpace(argument)
	if named != "" {
		if _, ok := m.session.Config.Providers[named]; !ok {
			return m.emit(m.printer.Note("no provider " + named + " · /providers lists them"))
		}
		providers = []string{named}
	}
	clients := map[string]*http.Client{}
	for _, name := range providers {
		clients[name] = m.session.Plugins.ProviderClient(name, m.session.Config.Providers[name])
	}
	settings := m.session.Config.Providers
	colors, barStyle := statusPalette(m.printer.isDark()), m.session.Config.Render.Bar
	note := printText(m.printer.Note("asking for the usage of the plans…"))
	return m, tea.Batch(note, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
		defer cancel()
		found := make([]providerUsage, len(providers))
		var wait sync.WaitGroup
		for i, name := range providers {
			wait.Go(func() { found[i] = askUsage(ctx, name, settings[name], clients[name]) })
		}
		wait.Wait()
		if named == "" {
			found = slices.DeleteFunc(found, func(usage providerUsage) bool { return errors.Is(usage.err, auth.ErrNeedsLogin) })
		}
		return planUsageMsg(drawUsage(found, colors, barStyle, time.Now()))
	})
}

// hasPlan tells a provider signed in to a plan with limits to ask about.
func hasPlan(settings config.ProviderConfig) bool {
	plan := settings.OAuth != nil || settings.TokenFrom != ""
	return plan && slices.Contains([]string{"claude-code", "responses", "cloudcode"}, settings.Protocol)
}

func askUsage(ctx context.Context, name string, settings config.ProviderConfig, client *http.Client) providerUsage {
	if !hasPlan(settings) {
		return providerUsage{provider: name, none: "no plan limits to ask about: paid by use, or local"}
	}
	token := auth.TokenFor(name, settings)
	var ask func(context.Context) ([]llm.Quota, error)
	switch settings.Protocol {
	case "claude-code":
		ask = (&llm.Anthropic{BaseURL: settings.BaseURL, Token: token, Subscription: true, Client: client}).Quotas
	case "responses":
		ask = (&llm.Responses{BaseURL: settings.BaseURL, Token: token, Client: client}).Quotas
	default:
		ask = (&llm.CloudCode{BaseURL: settings.BaseURL, Token: token, Client: client}).Quotas
	}
	quotas, err := ask(ctx)
	if err == nil && len(quotas) == 0 {
		return providerUsage{provider: name, none: "the plan says of no limits"}
	}
	return providerUsage{provider: name, quotas: quotas, err: err}
}

// drawUsage shows each provider's limits as bars, like the context's.
func drawUsage(found []providerUsage, colors statusColors, barStyle string, now time.Time) string {
	paint := func(c color.Color, text string) string { return lipgloss.NewStyle().Foreground(c).Render(text) }
	lines := []string{dimStyle.Render(noteMark + "usage of the plans")}
	width := 0
	for _, usage := range found {
		for _, quota := range usage.quotas {
			width = max(width, len(quota.Name))
		}
	}
	for _, usage := range found {
		switch {
		case usage.err != nil:
			lines = append(lines, "  "+paint(colors.provider, usage.provider)+"  "+paint(colors.high, ansi.Truncate(firstLine(usage.err.Error()), usageErrorRunes, "…")))
			continue
		case usage.none != "":
			lines = append(lines, "  "+paint(colors.provider, usage.provider)+"  "+paint(colors.label, usage.none))
			continue
		}
		lines = append(lines, "  "+paint(colors.provider, usage.provider))
		for _, quota := range usage.quotas {
			fill := colors.low
			switch {
			case quota.Used >= 0.8:
				fill = colors.high
			case quota.Used >= 0.5:
				fill = colors.mid
			}
			line := fmt.Sprintf("    %-*s  %s %s", width, quota.Name,
				contextBar(quota.Used, usageBarCells, fill, colors.empty, barStyle), paint(fill, fmt.Sprintf("%3.0f%%", quota.Used*100)))
			if !quota.Resets.IsZero() {
				line += "  " + paint(colors.label, resetsIn(quota.Resets, now))
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// resetsIn says when a limit starts over: how long from now, and the time.
func resetsIn(at, now time.Time) string {
	at = at.Local()
	when := at.Format("15:04")
	if at.YearDay() != now.YearDay() || at.Year() != now.Year() {
		when = at.Format("Mon 2 Jan 15:04")
	}
	left := at.Sub(now).Round(time.Minute)
	switch {
	case left <= 0:
		return "resets now"
	case left < time.Hour:
		return fmt.Sprintf("resets in %dm · %s", int(left.Minutes()), when)
	case left < 24*time.Hour:
		return fmt.Sprintf("resets in %dh %02dm · %s", int(left.Hours()), int(left.Minutes())%60, when)
	}
	return fmt.Sprintf("resets in %dd %dh · %s", int(left.Hours())/24, int(left.Hours())%24, when)
}
