package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Quota is how much of a limit of the plan is used: a window of time, or
// a model's share.
type Quota struct {
	// Name says which: 5h, week, a model.
	Name string
	// Used is the share used, 0 to 1.
	Used float64
	// Resets is when the limit starts over; zero when not said.
	Resets time.Time
}

// claudeWindows are the limits of a Claude plan mo shows, by the name the
// usage endpoint gives them; the others are codenames.
var claudeWindows = []struct{ key, name string }{
	{"five_hour", "5h"},
	{"seven_day", "week"},
	{"seven_day_opus", "week opus"},
	{"seven_day_sonnet", "week sonnet"},
}

// Quotas are the limits of the Claude plan signed in to, as Claude Code
// shows them in /usage.
func (p *Anthropic) Quotas(ctx context.Context) ([]Quota, error) {
	headers, err := p.headers(ctx)
	if err != nil {
		return nil, err
	}
	var usage map[string]json.RawMessage
	if err := getJSON(ctx, clientOrDefault(p.Client), strings.TrimRight(p.BaseURL, "/")+"/api/oauth/usage", headers, &usage); err != nil {
		return nil, err
	}
	var quotas []Quota
	for _, window := range claudeWindows {
		var limit struct {
			Utilization *float64  `json:"utilization"`
			ResetsAt    time.Time `json:"resets_at"`
		}
		if json.Unmarshal(usage[window.key], &limit) == nil && limit.Utilization != nil {
			quotas = append(quotas, Quota{Name: window.name, Used: *limit.Utilization / 100, Resets: limit.ResetsAt})
		}
	}
	return quotas, nil
}

type codexWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowSeconds int64   `json:"limit_window_seconds"`
	ResetAt       int64   `json:"reset_at"`
}

// Quotas are the limits of the ChatGPT plan signed in to, as Codex shows
// them in /status.
func (p *Responses) Quotas(ctx context.Context) ([]Quota, error) {
	headers, err := p.headers(ctx)
	if err != nil {
		return nil, err
	}
	var usage struct {
		RateLimit struct {
			Primary   *codexWindow `json:"primary_window"`
			Secondary *codexWindow `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	backend := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/codex")
	if err := getJSON(ctx, clientOrDefault(p.Client), backend+"/wham/usage", headers, &usage); err != nil {
		return nil, err
	}
	var quotas []Quota
	for _, window := range []*codexWindow{usage.RateLimit.Primary, usage.RateLimit.Secondary} {
		if window != nil {
			quotas = append(quotas, Quota{Name: windowName(window.WindowSeconds), Used: window.UsedPercent / 100, Resets: time.Unix(window.ResetAt, 0)})
		}
	}
	return quotas, nil
}

// windowName is a window of time as a person says it: 5h, week, 30d.
func windowName(seconds int64) string {
	const day = 24 * 60 * 60
	switch {
	case seconds == 7*day:
		return "week"
	case seconds%day == 0:
		return fmt.Sprintf("%dd", seconds/day)
	}
	return fmt.Sprintf("%dh", seconds/3600)
}

// Quotas are what is left of each model the account can use, as
// Antigravity shows it, or one line for all when they share a quota; the
// models with no reset are not limited.
func (p *CloudCode) Quotas(ctx context.Context) ([]Quota, error) {
	models, err := p.availableModels(ctx)
	if err != nil {
		return nil, err
	}
	var quotas []Quota
	for _, id := range slices.Sorted(maps.Keys(models)) {
		var model struct {
			QuotaInfo *struct {
				RemainingFraction float64   `json:"remainingFraction"`
				ResetTime         time.Time `json:"resetTime"`
			} `json:"quotaInfo"`
		}
		if json.Unmarshal(models[id], &model) != nil || model.QuotaInfo == nil || model.QuotaInfo.ResetTime.IsZero() {
			continue
		}
		quotas = append(quotas, Quota{Name: id, Used: 1 - model.QuotaInfo.RemainingFraction, Resets: model.QuotaInfo.ResetTime})
	}
	if len(quotas) > 1 && sharedQuota(quotas) {
		return []Quota{{Name: "all models", Used: quotas[0].Used, Resets: quotas[0].Resets}}, nil
	}
	return quotas, nil
}

func sharedQuota(quotas []Quota) bool {
	for _, quota := range quotas[1:] {
		if quota.Used != quotas[0].Used || !quota.Resets.Equal(quotas[0].Resets) {
			return false
		}
	}
	return true
}

func getJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return &HTTPError{Code: resp.StatusCode, Status: resp.Status, Header: resp.Header, Body: strings.TrimSpace(string(detail))}
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
