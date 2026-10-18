package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/jg/mo/internal/web"
)

const (
	fetchShownLines  = 200
	fetchCachedPages = 16
)

// pageCache keeps the pages fetched in this session, so reading on from
// another offset neither downloads the page again nor asks again.
type pageCache struct {
	mu    sync.Mutex
	pages map[string]web.Page
	order []string
}

func (c *pageCache) has(url string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.pages[url]
	return ok
}

func (c *pageCache) get(ctx context.Context, url string) (web.Page, error) {
	c.mu.Lock()
	page, ok := c.pages[url]
	c.mu.Unlock()
	if ok {
		return page, nil
	}
	page, err := web.Fetch(ctx, web.FetchClient, url)
	if err != nil {
		return web.Page{}, err
	}
	c.add(url, page)
	return page, nil
}

func (c *pageCache) add(url string, page web.Page) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.order) == fetchCachedPages {
		delete(c.pages, c.order[0])
		c.order = c.order[1:]
	}
	c.pages[url] = page
	c.order = append(c.order, url)
}

func fetchTool() Tool {
	cache := &pageCache{pages: map[string]web.Page{}}
	return Tool{
		Name:        "web_fetch",
		Description: "Fetch a URL as markdown, 200 lines unless limit says more.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`),
		Run: func(ctx context.Context, _ Env, raw json.RawMessage) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := decodeArgs(raw, &args); err != nil {
				return "", err
			}
			page, err := cache.get(ctx, args.URL)
			if err != nil {
				return "", err
			}
			header := page.URL + "\n\n"
			if page.Title != "" {
				header = "# " + page.Title + "\n" + header
			}
			return header + page.Markdown, nil
		},
		View:             true,
		ViewLimit:        fetchShownLines,
		Summary:          func(args json.RawMessage) string { return argField(args, "url") },
		AlreadyConfirmed: func(args json.RawMessage) bool { return cache.has(argField(args, "url")) },
	}
}

func webSearchTool(searcher web.Searcher) Tool {
	return Tool{
		Name:        "web_search",
		Description: "Search the web. Returns titles, URLs and snippets; read a result with web_fetch.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		Run: func(ctx context.Context, _ Env, raw json.RawMessage) (string, error) {
			if searcher == nil {
				return "", errors.New("web search is not configured")
			}
			var args struct {
				Query string `json:"query"`
			}
			if err := decodeArgs(raw, &args); err != nil {
				return "", err
			}
			results, err := searcher.Search(ctx, args.Query)
			if err != nil {
				return "", err
			}
			return web.FormatResults(results), nil
		},
		Summary: func(args json.RawMessage) string { return argField(args, "query") },
	}
}
