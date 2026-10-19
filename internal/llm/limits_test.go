package llm

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | LM-01 | Happy    | Claude's subscription limit is a plan limit with its window and reset    | P0       |
// | LM-02 | Happy    | Codex's usage_limit_reached is a plan limit with its reset               | P0       |
// | LM-03 | Happy    | OpenAI's insufficient_quota is a plan limit; rate_limit_exceeded a short one | P0   |
// | LM-04 | Happy    | Gemini's RESOURCE_EXHAUSTED is short with a short retryDelay, a quota without | P1   |
// | LM-05 | Happy    | Anthropic's overloaded_error and rate_limit_error are short, Retry-After read | P0    |
// | LM-06 | Edge     | A bare 429 is a short limit mo does not name; a 400 or 401 is no limit    | P0       |
// | LM-07 | Edge     | A short limit asking to wait over a minute counts as one of the plan       | P1       |
// | LM-08 | Happy    | Describe says the limit and when it lifts, today or another day           | P1       |
// | LM-09 | Happy    | Anthropic's limit errors in the middle of a stream are limits too         | P1       |

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

var limitNow = time.Date(2026, 10, 19, 17, 48, 0, 0, time.Local)

func limitOf(t *testing.T, code int, header map[string]string, body string) (Limit, bool) {
	t.Helper()
	headers := http.Header{}
	for name, value := range header {
		headers.Set(name, value)
	}
	return classifyLimit(&HTTPError{Code: code, Status: http.StatusText(code), Header: headers, Body: body}, limitNow)
}

func TestLimits(t *testing.T) {
	t.Run("LM-01 Claude's subscription limit is a plan limit with its window and reset", func(t *testing.T) {
		reset := limitNow.Add(112 * time.Minute)

		limit, ok := limitOf(t, 429, map[string]string{
			"anthropic-ratelimit-unified-status":               "rejected",
			"anthropic-ratelimit-unified-representative-claim": "seven_day",
			"anthropic-ratelimit-unified-reset":                fmtUnix(reset),
		}, `{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit."}}`)

		if !ok || !limit.Plan || !limit.Known || limit.Reason != "weekly usage limit reached" || !limit.Reset.Equal(reset) {
			t.Errorf("limit = %+v %v", limit, ok)
		}
	})

	t.Run("LM-02 Codex's usage_limit_reached is a plan limit with its reset", func(t *testing.T) {
		limit, ok := limitOf(t, 429, nil, `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"plus","resets_in_seconds":3600}}`)

		if !ok || !limit.Plan || !limit.Known || limit.Reason != "usage limit reached" || !limit.Reset.Equal(limitNow.Add(time.Hour)) {
			t.Errorf("limit = %+v %v", limit, ok)
		}
	})

	t.Run("LM-03 OpenAI's insufficient_quota is a plan limit; rate_limit_exceeded a short one", func(t *testing.T) {
		quota, quotaOK := limitOf(t, 429, nil, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`)
		rate, rateOK := limitOf(t, 429, map[string]string{"Retry-After": "6"}, `{"error":{"message":"Rate limit reached","type":"requests","code":"rate_limit_exceeded"}}`)

		if !quotaOK || !quota.Plan || !strings.HasPrefix(quota.Reason, "out of credits") {
			t.Errorf("quota = %+v", quota)
		}
		if !rateOK || rate.Plan || !rate.Known || rate.Reason != "rate limited" || rate.RetryAfter != 6*time.Second {
			t.Errorf("rate = %+v", rate)
		}
	})

	t.Run("LM-04 Gemini's RESOURCE_EXHAUSTED is short with a short retryDelay, a quota without", func(t *testing.T) {
		short, _ := limitOf(t, 429, nil, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"34s"}]}}`)
		daily, _ := limitOf(t, 429, nil, `[{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded"}}]`)

		if short.Plan || short.RetryAfter != 34*time.Second || short.Reason != "rate limited" {
			t.Errorf("short = %+v", short)
		}
		if !daily.Plan || !daily.Known || daily.Reason != "quota exhausted" {
			t.Errorf("daily = %+v", daily)
		}
	})

	t.Run("LM-05 Anthropic's overloaded_error and rate_limit_error are short, Retry-After read", func(t *testing.T) {
		overloaded, _ := limitOf(t, 529, nil, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		rate, _ := limitOf(t, 429, map[string]string{"retry-after": "12"}, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)

		if overloaded.Plan || !overloaded.Known || overloaded.Reason != "overloaded" {
			t.Errorf("overloaded = %+v", overloaded)
		}
		if rate.Plan || rate.Reason != "rate limited" || rate.RetryAfter != 12*time.Second {
			t.Errorf("rate = %+v", rate)
		}
	})

	t.Run("LM-06 a bare 429 is a short limit mo does not name; a 400 or 401 is no limit", func(t *testing.T) {
		bare, bareOK := limitOf(t, 429, nil, `slow down please`)
		_, badOK := limitOf(t, 400, nil, `{"error":{"type":"invalid_request_error"}}`)
		_, authOK := limitOf(t, 401, nil, `{"error":{"type":"authentication_error"}}`)

		if !bareOK || bare.Known || bare.Plan || badOK || authOK {
			t.Errorf("bare = %+v %v bad %v auth %v", bare, bareOK, badOK, authOK)
		}
	})

	t.Run("LM-07 a short limit asking to wait over a minute counts as one of the plan", func(t *testing.T) {
		limit, _ := limitOf(t, 429, map[string]string{"Retry-After": "600"}, `{"error":{"code":"rate_limit_exceeded"}}`)

		if !limit.Plan || !limit.Reset.Equal(limitNow.Add(10*time.Minute)) {
			t.Errorf("limit = %+v", limit)
		}
	})

	t.Run("LM-08 Describe says the limit and when it lifts, today or another day", func(t *testing.T) {
		today := Limit{Reason: "weekly usage limit reached", Reset: limitNow.Add(112 * time.Minute)}
		later := Limit{Reason: "usage limit reached", Reset: limitNow.Add(51 * time.Hour)}
		none := Limit{Reason: "out of credits"}

		if got := today.Describe("claude", limitNow); got != "claude: weekly usage limit reached · resets at 19:40 (in 1 h 52 min)" {
			t.Errorf("today = %q", got)
		}
		if got := later.Describe("codex", limitNow); got != "codex: usage limit reached · resets at Wed 21 Oct 20:48 (in 2 d 3 h)" {
			t.Errorf("later = %q", got)
		}
		if got := none.Describe("openai", limitNow); got != "openai: out of credits" {
			t.Errorf("none = %q", got)
		}
	})

	t.Run("LM-09 Anthropic's limit errors in the middle of a stream are limits too", func(t *testing.T) {
		server := sseServer(t, []string{`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`}, nil)

		_, err := (&Anthropic{BaseURL: server.URL, APIKey: "k"}).Stream(context.Background(), Request{Model: "m"}, func(Event) {})

		limit, ok := LimitOf(err)
		if !ok || limit.Reason != "overloaded" || limit.Plan {
			t.Errorf("err = %v limit = %+v", err, limit)
		}
	})
}

func fmtUnix(at time.Time) string {
	return strconv.FormatInt(at.Unix(), 10)
}
