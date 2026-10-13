package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	braveURL  = "https://api.search.brave.com/res/v1/web/search"
	tavilyURL = "https://api.tavily.com/search"
)

type searXNG struct {
	client  *http.Client
	baseURL string
}

func (s *searXNG) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint := strings.TrimRight(s.baseURL, "/") + "/search?" + url.Values{"q": {query}, "format": {"json"}}.Encode()
	var answer struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := getJSON(ctx, s.client, endpoint, nil, &answer); err != nil {
		return nil, fmt.Errorf("searxng: %w", err)
	}
	var results []Result
	for _, item := range answer.Results {
		results = append(results, Result{Title: item.Title, URL: item.URL, Snippet: item.Content})
	}
	return results, nil
}

type brave struct {
	client   *http.Client
	key      string
	endpoint string
}

func (b *brave) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint := b.endpoint + "?" + url.Values{"q": {query}, "count": {fmt.Sprint(searchMaxResults)}}.Encode()
	var answer struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	headers := map[string]string{"X-Subscription-Token": b.key}
	if err := getJSON(ctx, b.client, endpoint, headers, &answer); err != nil {
		return nil, fmt.Errorf("brave: %w", err)
	}
	var results []Result
	for _, item := range answer.Web.Results {
		results = append(results, Result{Title: item.Title, URL: item.URL, Snippet: item.Description})
	}
	return results, nil
}

type tavily struct {
	client   *http.Client
	key      string
	endpoint string
}

func (t *tavily) Search(ctx context.Context, query string) ([]Result, error) {
	payload, _ := json.Marshal(map[string]any{"query": query, "max_results": searchMaxResults})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.key)
	var answer struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := doJSON(t.client, req, &answer); err != nil {
		return nil, fmt.Errorf("tavily: %w", err)
	}
	var results []Result
	for _, item := range answer.Results {
		results = append(results, Result{Title: item.Title, URL: item.URL, Snippet: item.Content})
	}
	return results, nil
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return doJSON(client, req, into)
}

func doJSON(client *http.Client, req *http.Request, into any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, searchMaxBytes)).Decode(into)
}
