package llm

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | QU-01 | Happy    | Claude's 5h and week windows, with the sign-in's headers; codenames left | P0       |
// | QU-02 | Happy    | Codex's windows named by their length, from the backend's /wham/usage    | P0       |
// | QU-03 | Happy    | Antigravity's models sharing one quota are one line; unlimited ones none | P1       |
// | QU-04 | Happy    | Antigravity's models with quotas of their own are a line each            | P1       |
// | QU-05 | Error    | A refused request is an error with its status                            | P1       |

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func quotaServer(t *testing.T, body string, status int, got *recorded) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header, got.path = r.Header, r.URL.Path
		if strings.Contains(r.URL.Path, "loadCodeAssist") {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func bearer(context.Context) (string, error) { return "tok", nil }

func TestQuotas(t *testing.T) {
	t.Run("QU-01 Claude's 5h and week windows, with the sign-in's headers; codenames left", func(t *testing.T) {
		var got recorded
		url := quotaServer(t, `{"five_hour":{"utilization":8.0,"resets_at":"2026-10-28T08:59:59Z"},
			"seven_day":{"utilization":12.0,"resets_at":"2026-11-03T12:59:59Z"},"seven_day_opus":null,
			"iguana_necktie":{"utilization":50},"limits":[1]}`, http.StatusOK, &got)

		quotas, err := (&Anthropic{BaseURL: url, Token: bearer, Subscription: true}).Quotas(context.Background())

		want := []Quota{
			{Name: "5h", Used: 0.08, Resets: time.Date(2026, 10, 28, 8, 59, 59, 0, time.UTC)},
			{Name: "week", Used: 0.12, Resets: time.Date(2026, 11, 3, 12, 59, 59, 0, time.UTC)},
		}
		if err != nil || len(quotas) != 2 || quotas[0] != want[0] || quotas[1] != want[1] {
			t.Fatalf("quotas = %+v, err = %v", quotas, err)
		}
		if got.path != "/api/oauth/usage" || got.header.Get("Authorization") != "Bearer tok" || got.header.Get("anthropic-beta") != claudeCodeBeta {
			t.Errorf("path %q, headers %v", got.path, got.header)
		}
	})

	t.Run("QU-02 Codex's windows named by their length, from the backend's /wham/usage", func(t *testing.T) {
		var got recorded
		url := quotaServer(t, `{"rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_at":1800000000},
			"secondary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_at":1800500000}}}`, http.StatusOK, &got)

		quotas, err := (&Responses{BaseURL: url + "/backend-api/codex", Token: bearer}).Quotas(context.Background())

		if err != nil || len(quotas) != 2 || quotas[0].Name != "5h" || quotas[0].Used != 0.25 || quotas[1].Name != "week" || !quotas[1].Resets.Equal(time.Unix(1800500000, 0)) {
			t.Fatalf("quotas = %+v, err = %v", quotas, err)
		}
		if got.path != "/backend-api/wham/usage" {
			t.Errorf("path %q", got.path)
		}
		if windowName(30*24*3600) != "30d" {
			t.Errorf("30 days named %q", windowName(30*24*3600))
		}
	})

	t.Run("QU-03 Antigravity's models sharing one quota are one line; unlimited ones none", func(t *testing.T) {
		var got recorded
		url := quotaServer(t, `{"models":{
			"gemini-a":{"quotaInfo":{"remainingFraction":0.75,"resetTime":"2026-11-04T08:33:19Z"}},
			"gemini-b":{"quotaInfo":{"remainingFraction":0.75,"resetTime":"2026-11-04T08:33:19Z"}},
			"tab":{"quotaInfo":{"remainingFraction":1}}}}`, http.StatusOK, &got)

		quotas, err := (&CloudCode{BaseURL: url, Token: bearer}).Quotas(context.Background())

		if err != nil || len(quotas) != 1 || quotas[0].Name != "all models" || quotas[0].Used != 0.25 {
			t.Fatalf("quotas = %+v, err = %v", quotas, err)
		}
	})

	t.Run("QU-04 Antigravity's models with quotas of their own are a line each", func(t *testing.T) {
		var got recorded
		url := quotaServer(t, `{"models":{
			"gemini-b":{"quotaInfo":{"remainingFraction":0.5,"resetTime":"2026-11-04T08:33:19Z"}},
			"gemini-a":{"quotaInfo":{"remainingFraction":1,"resetTime":"2026-11-04T08:33:19Z"}}}}`, http.StatusOK, &got)

		quotas, err := (&CloudCode{BaseURL: url, Token: bearer}).Quotas(context.Background())

		if err != nil || len(quotas) != 2 || quotas[0].Name != "gemini-a" || quotas[1].Name != "gemini-b" || quotas[1].Used != 0.5 {
			t.Fatalf("quotas = %+v, err = %v", quotas, err)
		}
	})

	t.Run("QU-05 a refused request is an error with its status", func(t *testing.T) {
		var got recorded
		url := quotaServer(t, `{"error":"expired"}`, http.StatusUnauthorized, &got)

		_, err := (&Anthropic{BaseURL: url, Token: bearer, Subscription: true}).Quotas(context.Background())

		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("err = %v", err)
		}
	})
}
