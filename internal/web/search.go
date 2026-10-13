package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	SearchDuckDuckGo = "duckduckgo"
	SearchSearXNG    = "searxng"
	SearchBrave      = "brave"
	SearchTavily     = "tavily"

	searchMaxResults = 8
	searchTimeout    = 30 * time.Second
	searchMaxBytes   = 2 << 20
)

type Result struct {
	Title   string
	URL     string
	Snippet string
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

type SearchSettings struct {
	Backend string
	URL     string
	Key     string
}

func NewSearcher(settings SearchSettings, client *http.Client) (Searcher, error) {
	switch settings.Backend {
	case SearchDuckDuckGo, "":
		return &duckDuckGo{client: client}, nil
	case SearchSearXNG:
		if settings.URL == "" {
			return nil, fmt.Errorf("searxng needs web.search_url")
		}
		return &searXNG{client: client, baseURL: settings.URL}, nil
	case SearchBrave:
		if settings.Key == "" {
			return nil, missingKey(settings.Backend)
		}
		return &brave{client: client, key: settings.Key, endpoint: braveURL}, nil
	case SearchTavily:
		if settings.Key == "" {
			return nil, missingKey(settings.Backend)
		}
		return &tavily{client: client, key: settings.Key, endpoint: tavilyURL}, nil
	}
	return nil, fmt.Errorf("unknown web.search %q: use duckduckgo, searxng, brave or tavily", settings.Backend)
}

// SearchClient never follows a redirect: a search engine that redirects,
// like a DuckDuckGo !bang, would carry the query to another site.
var SearchClient = &http.Client{
	Timeout:       searchTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func missingKey(backend string) error {
	return fmt.Errorf("%s needs web.search_key or web.search_key_env", backend)
}

// FormatResults lists results as numbered markdown, the shortest form that
// keeps title, address and snippet.
func FormatResults(results []Result) string {
	if len(results) == 0 {
		return "no results"
	}
	var out strings.Builder
	for i, result := range results[:min(len(results), searchMaxResults)] {
		fmt.Fprintf(&out, "%d. [%s](%s)\n", i+1, oneLine(result.Title), result.URL)
		if snippet := oneLine(result.Snippet); snippet != "" {
			fmt.Fprintf(&out, "   %s\n", snippet)
		}
	}
	return out.String()
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
