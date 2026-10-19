package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// shortWait is the longest wait worth retrying for; a limit that asks for
// more is told instead.
const shortWait = time.Minute

// Limit is a provider refusing a request for a limit: of the account or
// the plan, which waiting a moment does not lift, or a short one.
type Limit struct {
	// Plan is a limit of the account or the plan: no use retrying soon.
	Plan bool
	// Known is a limit the provider names in a way mo recognises, so it is
	// said plainly; any other shows as the provider sent it.
	Known bool
	// Reason says what limit, in a few words.
	Reason string
	// Reset is when the limit lifts, when the provider says.
	Reset time.Time
	// RetryAfter is how long to wait before trying again, when the
	// provider says.
	RetryAfter time.Duration
}

// LimitOf tells whether err is a provider refusing for a limit, and which.
func LimitOf(err error) (Limit, bool) {
	var failed *HTTPError
	if !errors.As(err, &failed) {
		return Limit{}, false
	}
	return classifyLimit(failed, time.Now())
}

// providerError is the error body of Anthropic, OpenAI, Codex and Gemini,
// each filling its own fields.
type providerError struct {
	Type            string          `json:"type"`
	Code            json.RawMessage `json:"code"`
	Status          string          `json:"status"`
	ResetsAt        float64         `json:"resets_at"`
	ResetsInSeconds float64         `json:"resets_in_seconds"`
	Details         []struct {
		Type       string `json:"@type"`
		RetryDelay string `json:"retryDelay"`
	} `json:"details"`
}

func classifyLimit(failed *HTTPError, now time.Time) (Limit, bool) {
	body := parseProviderError(failed.Body)
	limit := Limit{RetryAfter: retryAfter(failed.Header, now)}
	code := strings.Trim(string(body.Code), `"`)
	switch {
	case failed.Header.Get("Anthropic-Ratelimit-Unified-Status") == "rejected":
		limit.Plan, limit.Known = true, true
		limit.Reason = anthropicClaim(failed.Header.Get("Anthropic-Ratelimit-Unified-Representative-Claim"))
		limit.Reset = unixTime(failed.Header.Get("Anthropic-Ratelimit-Unified-Reset"))
	case body.Type == "usage_limit_reached":
		limit.Plan, limit.Known, limit.Reason = true, true, "usage limit reached"
		limit.Reset = codexReset(body, now)
	case body.Type == "insufficient_quota" || code == "insufficient_quota":
		limit.Plan, limit.Known, limit.Reason = true, true, "out of credits: check the account's billing"
	case body.Status == "RESOURCE_EXHAUSTED":
		limit.Known, limit.Reason = true, "quota exhausted"
		if delay := geminiRetryDelay(body); delay > 0 {
			limit.RetryAfter = delay
		}
		if limit.RetryAfter == 0 || limit.RetryAfter > shortWait {
			limit.Plan = true
		} else {
			limit.Reason = "rate limited"
		}
	case body.Type == "overloaded_error" || body.Status == "UNAVAILABLE":
		limit.Known, limit.Reason = true, "overloaded"
	case body.Type == "rate_limit_error" || code == "rate_limit_exceeded":
		limit.Known, limit.Reason = true, "rate limited"
	case failed.Code == http.StatusTooManyRequests || failed.Code == 529 || failed.Code == http.StatusServiceUnavailable:
		limit.Reason = "rate limited or overloaded"
	default:
		return Limit{}, false
	}
	if !limit.Plan && limit.RetryAfter > shortWait {
		limit.Plan = true
	}
	if limit.Plan && limit.Reset.IsZero() && limit.RetryAfter > 0 {
		limit.Reset = now.Add(limit.RetryAfter)
	}
	return limit, true
}

// parseProviderError reads {"error": {...}}, or a list of them as Gemini
// sends.
func parseProviderError(text string) providerError {
	var one struct {
		Error providerError `json:"error"`
	}
	if strings.HasPrefix(strings.TrimSpace(text), "[") {
		var many []struct {
			Error providerError `json:"error"`
		}
		if json.Unmarshal([]byte(text), &many) == nil && len(many) > 0 {
			return many[0].Error
		}
		return providerError{}
	}
	_ = json.Unmarshal([]byte(text), &one)
	return one.Error
}

// retryAfter is the wait the Retry-After header asks for, in seconds or
// as a date.
func retryAfter(header http.Header, now time.Time) time.Duration {
	value := header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(seconds * float64(time.Second))
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func anthropicClaim(claim string) string {
	switch {
	case strings.HasPrefix(claim, "five_hour"):
		return "5-hour usage limit reached"
	case strings.HasPrefix(claim, "seven_day"):
		return "weekly usage limit reached"
	}
	return "usage limit reached"
}

func unixTime(value string) time.Time {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0)
}

func codexReset(body providerError, now time.Time) time.Time {
	if body.ResetsAt > 0 {
		return time.Unix(int64(body.ResetsAt), 0)
	}
	if body.ResetsInSeconds > 0 {
		return now.Add(time.Duration(body.ResetsInSeconds * float64(time.Second)))
	}
	return time.Time{}
}

func geminiRetryDelay(body providerError) time.Duration {
	for _, detail := range body.Details {
		if !strings.HasSuffix(detail.Type, "RetryInfo") {
			continue
		}
		if delay, err := time.ParseDuration(detail.RetryDelay); err == nil {
			return delay
		}
	}
	return 0
}

// Describe says the limit plainly, after the provider's name: what it is
// and, when known, when it lifts.
func (l Limit) Describe(provider string, now time.Time) string {
	text := provider + ": " + l.Reason
	if l.Reset.IsZero() || !l.Reset.After(now) {
		return text
	}
	at := l.Reset.Local().Format("15:04")
	if !sameDay(l.Reset.Local(), now.Local()) {
		at = l.Reset.Local().Format("Mon 2 Jan 15:04")
	}
	return fmt.Sprintf("%s · resets at %s (in %s)", text, at, Span(l.Reset.Sub(now)))
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// Span is a wait as it reads best: "2 d 3 h", "1 h 52 min", "45 min",
// "30 s".
func Span(wait time.Duration) string {
	wait = wait.Round(time.Second)
	days := int(wait.Hours()) / 24
	hours := int(wait.Hours()) % 24
	minutes := int(wait.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%d d %d h", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d h %d min", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d s", int(wait.Seconds()))
}
