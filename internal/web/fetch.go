// Package web fetches pages as markdown and searches the web.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

const (
	fetchTimeout  = 30 * time.Second
	fetchMaxBytes = 5 << 20
	maxRedirects  = 10
	userAgent     = "Mozilla/5.0 (compatible; mo/0.1; terminal coding agent)"
)

type Page struct {
	URL      string
	Title    string
	Markdown string
}

// FetchClient follows redirects only within the same host. A redirect to
// another host ends the fetch with its target, to be fetched, and approved,
// on its own.
var FetchClient = &http.Client{CheckRedirect: sameHostRedirects}

func sameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("redirected to %s; fetch that URL to follow it", req.URL)
	}
	return nil
}

// Fetch downloads a page and turns it into markdown: the main content of an
// HTML page, indented JSON, or text as it is.
func Fetch(ctx context.Context, client *http.Client, rawURL string) (Page, error) {
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return Page{}, fmt.Errorf("not an http(s) URL: %q", rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,text/plain;q=0.9,*/*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Page{}, fmt.Errorf("%s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes+1))
	if err != nil {
		return Page{}, err
	}
	cut := len(body) > fetchMaxBytes
	if cut {
		body = body[:fetchMaxBytes]
	}

	page := Page{URL: resp.Request.URL.String()}
	page.Title, page.Markdown, err = toMarkdown(body, resp.Header.Get("Content-Type"), resp.Request.URL)
	if err != nil {
		return Page{}, err
	}
	if cut {
		page.Markdown += fmt.Sprintf("\n\n[page cut at %d MB]", fetchMaxBytes>>20)
	}
	return page, nil
}

var textMediaTypes = []string{
	"application/xml", "application/javascript", "application/x-javascript",
	"application/yaml", "application/x-yaml", "application/toml", "application/x-sh",
}

func toMarkdown(body []byte, contentType string, pageURL *url.URL) (title, markdown string, err error) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml" || (mediaType == "" && looksLikeHTML(body)):
		return htmlToMarkdown(body, pageURL)
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		var indented bytes.Buffer
		if json.Indent(&indented, body, "", "  ") != nil {
			return "", string(body), nil
		}
		return "", "```json\n" + indented.String() + "\n```", nil
	case strings.HasPrefix(mediaType, "text/") || mediaType == "" || strings.HasSuffix(mediaType, "+xml") || slices.Contains(textMediaTypes, mediaType):
		return "", string(body), nil
	}
	return "", "", fmt.Errorf("cannot read %s content", mediaType)
}

// htmlToMarkdown keeps the article when readability finds one, else the
// whole page. Links stay absolute. HTML too deeply nested to parse falls
// back to its bare text.
func htmlToMarkdown(body []byte, pageURL *url.URL) (title, markdown string, err error) {
	pageHTML := string(body)
	article, err := readability.FromReader(bytes.NewReader(body), pageURL)
	if err == nil && article.Node != nil {
		var rendered strings.Builder
		if article.RenderHTML(&rendered) == nil {
			pageHTML = rendered.String()
			title = article.Title()
		}
	}
	markdown, err = htmltomarkdown.ConvertString(pageHTML, converter.WithDomain(pageURL.Scheme+"://"+pageURL.Host))
	if err != nil {
		markdown = bareText(body)
	}
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return "", "", errors.New("the page has no readable text")
	}
	return title, markdown, nil
}

// bareText collects the text of an HTML document with the tokenizer, which,
// unlike the parser, has no limit on nesting.
func bareText(body []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var text strings.Builder
	skipping := false
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return collapseBlankLines(text.String())
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			skipping = string(name) == "script" || string(name) == "style"
		case html.EndTagToken:
			skipping = false
			text.WriteByte('\n')
		case html.TextToken:
			if !skipping {
				text.WriteString(oneLine(string(tokenizer.Text())) + " ")
			}
		}
	}
}

func collapseBlankLines(text string) string {
	var lines []string
	for line := range strings.Lines(text) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

func looksLikeHTML(body []byte) bool {
	start := bytes.ToLower(bytes.TrimSpace(body[:min(len(body), 512)]))
	return bytes.HasPrefix(start, []byte("<!doctype html")) || bytes.HasPrefix(start, []byte("<html"))
}
