package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const duckDuckGoURL = "https://html.duckduckgo.com/html/"

// duckDuckGo reads the results of DuckDuckGo's plain HTML page, which needs
// no key. It may answer with a bot check instead.
type duckDuckGo struct {
	client  *http.Client
	baseURL string
}

func (d *duckDuckGo) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint := d.baseURL
	if endpoint == "" {
		endpoint = duckDuckGoURL
	}
	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("duckduckgo: %s", resp.Status)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, searchMaxBytes))
	if err != nil {
		return nil, err
	}
	results := duckDuckGoResults(doc)
	if len(results) == 0 && hasClass(doc, "anomaly-modal") {
		return nil, errors.New("duckduckgo asked for a bot check; set web.search to searxng, brave or tavily")
	}
	return results, nil
}

func duckDuckGoResults(doc *html.Node) []Result {
	var results []Result
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode || node.Data != "div" || !nodeHasClass(node, "result") {
			continue
		}
		var result Result
		for child := range node.Descendants() {
			switch {
			case nodeHasClass(child, "result__a"):
				result.Title = textOf(child)
				result.URL = duckDuckGoTarget(attribute(child, "href"))
			case nodeHasClass(child, "result__snippet"):
				result.Snippet = textOf(child)
			}
		}
		if result.URL != "" {
			results = append(results, result)
		}
	}
	return results
}

// duckDuckGoTarget unwraps the redirect links of the result page.
func duckDuckGoTarget(href string) string {
	link, err := url.Parse(href)
	if err != nil {
		return href
	}
	if target := link.Query().Get("uddg"); target != "" {
		return target
	}
	if link.Scheme == "" {
		link.Scheme = "https"
	}
	return link.String()
}

func hasClass(doc *html.Node, class string) bool {
	for node := range doc.Descendants() {
		if nodeHasClass(node, class) {
			return true
		}
	}
	return false
}

func nodeHasClass(node *html.Node, class string) bool {
	return node.Type == html.ElementNode && strings.Contains(" "+attribute(node, "class")+" ", " "+class+" ")
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func textOf(node *html.Node) string {
	var text strings.Builder
	for child := range node.Descendants() {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}
	return oneLine(text.String())
}
