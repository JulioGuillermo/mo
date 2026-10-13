package web

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | WB-01 | Happy    | An HTML article becomes markdown with its title and absolute links   | P0       |
// | WB-02 | Happy    | JSON comes back indented in a code block                             | P1       |
// | WB-03 | Error    | Binary content is refused                                            | P1       |
// | WB-04 | Error    | A non-http URL is refused before any request                         | P0       |
// | WB-05 | Error    | An HTTP error status is an error                                     | P1       |
// | WB-06 | Edge     | A page over the size limit is cut and says so                        | P2       |
// | WB-07 | Happy    | DuckDuckGo results are read and their redirect links unwrapped       | P0       |
// | WB-08 | Error    | A DuckDuckGo bot check is reported with what to configure instead    | P1       |
// | WB-09 | Happy    | SearXNG, Brave and Tavily answers are read; keys go in their headers | P1       |
// | WB-10 | Error    | Backends that need a key or URL refuse to start without it           | P1       |
// | WB-11 | Happy    | Results are formatted as numbered markdown links on single lines     | P2       |
// | WB-12 | Security | A redirect to another host stops the fetch and names the target      | P0       |
// | WB-13 | Happy    | A redirect within the same host is followed                          | P1       |
// | WB-14 | Security | The search client never follows a redirect (DuckDuckGo !bangs)       | P0       |
// | WB-15 | Happy    | XML, YAML and JavaScript come back as text                           | P2       |
// | WB-16 | Edge     | HTML nested too deep to parse still gives its text                   | P2       |

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const articleHTML = `<!doctype html><html><head><title>Release notes</title></head><body>
<nav><a href="/">Home</a> <a href="/blog">Blog</a></nav>
<article><h1>Release notes</h1>
<p>This release brings a faster compiler and many fixes for the runtime, the linker and the tools.</p>
<p>Read the <a href="/doc/changes">full list of changes</a> for every detail of what moved and why it matters.</p>
<p>Thanks to everyone who reported bugs, sent patches and tested the release candidates over the last months.</p>
</article><footer>Copyright</footer></body></html>`

func serve(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetch(t *testing.T) {
	ctx := context.Background()

	t.Run("WB-01 an HTML article becomes markdown with its title and absolute links", func(t *testing.T) {
		server := serve(t, "text/html; charset=utf-8", articleHTML)

		page, err := Fetch(ctx, http.DefaultClient, server.URL+"/notes")

		if err != nil {
			t.Fatal(err)
		}
		if page.Title != "Release notes" || !strings.Contains(page.Markdown, "faster compiler") {
			t.Errorf("page = %+v", page)
		}
		if !strings.Contains(page.Markdown, "]("+server.URL+"/doc/changes)") {
			t.Errorf("link not absolute: %s", page.Markdown)
		}
		if strings.Contains(page.Markdown, "Copyright") {
			t.Errorf("the footer was kept: %s", page.Markdown)
		}
	})

	t.Run("WB-02 JSON comes back indented in a code block", func(t *testing.T) {
		server := serve(t, "application/json", `{"a":1,"b":[2,3]}`)

		page, err := Fetch(ctx, http.DefaultClient, server.URL)

		if err != nil || !strings.HasPrefix(page.Markdown, "```json\n{\n  \"a\": 1") {
			t.Errorf("markdown = %q err = %v", page.Markdown, err)
		}
	})

	t.Run("WB-03 binary content is refused", func(t *testing.T) {
		server := serve(t, "application/octet-stream", "\x00\x01")

		if _, err := Fetch(ctx, http.DefaultClient, server.URL); err == nil || !strings.Contains(err.Error(), "cannot read") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("WB-04 a non-http URL is refused before any request", func(t *testing.T) {
		for _, target := range []string{"file:///etc/passwd", "ftp://x/y", "not a url"} {
			if _, err := Fetch(ctx, http.DefaultClient, target); err == nil {
				t.Errorf("%s was fetched", target)
			}
		}
	})

	t.Run("WB-05 an HTTP error status is an error", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()

		if _, err := Fetch(ctx, http.DefaultClient, server.URL); err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("WB-06 a page over the size limit is cut and says so", func(t *testing.T) {
		server := serve(t, "text/plain", strings.Repeat("x", fetchMaxBytes+10))

		page, err := Fetch(ctx, http.DefaultClient, server.URL)

		if err != nil || !strings.HasSuffix(page.Markdown, "[page cut at 5 MB]") {
			t.Errorf("tail = %q err = %v", page.Markdown[len(page.Markdown)-30:], err)
		}
	})
}

const duckDuckGoPage = `<html><body>
<div class="result results_links web-result">
  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&amp;rut=x">The Go <b>docs</b></a>
  <a class="result__snippet" href="#">Everything about   Go.</a>
</div>
<div class="result results_links web-result">
  <a class="result__a" href="https://pkg.go.dev/">pkg.go.dev</a>
</div>
</body></html>`

func TestDuckDuckGo(t *testing.T) {
	t.Run("WB-07 DuckDuckGo results are read and their redirect links unwrapped", func(t *testing.T) {
		var query string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.ParseForm()
			query = r.Form.Get("q")
			fmt.Fprint(w, duckDuckGoPage)
		}))
		defer server.Close()

		results, err := (&duckDuckGo{client: http.DefaultClient, baseURL: server.URL}).Search(context.Background(), "go docs")

		if err != nil || query != "go docs" || len(results) != 2 {
			t.Fatalf("results = %+v query = %q err = %v", results, query, err)
		}
		if results[0] != (Result{Title: "The Go docs", URL: "https://go.dev/doc/", Snippet: "Everything about Go."}) {
			t.Errorf("first = %+v", results[0])
		}
	})

	t.Run("WB-08 a DuckDuckGo bot check is reported with what to configure instead", func(t *testing.T) {
		server := serve(t, "text/html", `<html><body><div class="anomaly-modal">Are you a robot?</div></body></html>`)

		_, err := (&duckDuckGo{client: http.DefaultClient, baseURL: server.URL}).Search(context.Background(), "x")

		if err == nil || !strings.Contains(err.Error(), "searxng") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestSearchAPIs(t *testing.T) {
	t.Run("WB-09 SearXNG, Brave and Tavily answers are read; keys go in their headers", func(t *testing.T) {
		var braveKey, tavilyAuth, tavilyQuery string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/search":
				fmt.Fprint(w, `{"results":[{"title":"S","url":"https://s.example","content":"from searxng"}]}`)
			case "/brave":
				braveKey = r.Header.Get("X-Subscription-Token")
				fmt.Fprint(w, `{"web":{"results":[{"title":"B","url":"https://b.example","description":"from brave"}]}}`)
			case "/tavily":
				tavilyAuth = r.Header.Get("Authorization")
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				tavilyQuery, _ = body["query"].(string)
				fmt.Fprint(w, `{"results":[{"title":"T","url":"https://t.example","content":"from tavily"}]}`)
			}
		}))
		defer server.Close()
		ctx := context.Background()

		searx, _ := (&searXNG{client: http.DefaultClient, baseURL: server.URL}).Search(ctx, "q")
		braveResults, _ := (&brave{client: http.DefaultClient, key: "bk", endpoint: server.URL + "/brave"}).Search(ctx, "q")
		tavilyResults, _ := (&tavily{client: http.DefaultClient, key: "tk", endpoint: server.URL + "/tavily"}).Search(ctx, "find me")

		if len(searx) != 1 || searx[0].Snippet != "from searxng" {
			t.Errorf("searxng = %+v", searx)
		}
		if len(braveResults) != 1 || braveResults[0].Snippet != "from brave" || braveKey != "bk" {
			t.Errorf("brave = %+v key = %q", braveResults, braveKey)
		}
		if len(tavilyResults) != 1 || tavilyAuth != "Bearer tk" || tavilyQuery != "find me" {
			t.Errorf("tavily = %+v auth = %q query = %q", tavilyResults, tavilyAuth, tavilyQuery)
		}
	})

	t.Run("WB-10 backends that need a key or URL refuse to start without it", func(t *testing.T) {
		for _, settings := range []SearchSettings{{Backend: SearchSearXNG}, {Backend: SearchBrave}, {Backend: SearchTavily}, {Backend: "bing"}} {
			if _, err := NewSearcher(settings, http.DefaultClient); err == nil {
				t.Errorf("%+v was accepted", settings)
			}
		}
		if searcher, err := NewSearcher(SearchSettings{}, http.DefaultClient); err != nil || searcher == nil {
			t.Errorf("the default backend failed: %v", err)
		}
	})

	t.Run("WB-11 results are formatted as numbered markdown links on single lines", func(t *testing.T) {
		text := FormatResults([]Result{{Title: "A\ntitle", URL: "https://a", Snippet: "one\n  two"}, {Title: "B", URL: "https://b"}})

		if text != "1. [A title](https://a)\n   one two\n2. [B](https://b)\n" {
			t.Errorf("text = %q", text)
		}
	})
}

func TestRedirects(t *testing.T) {
	ctx := context.Background()

	t.Run("WB-12 a redirect to another host stops the fetch and names the target", func(t *testing.T) {
		internal := serve(t, "text/plain", "internal secret")
		public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, strings.Replace(internal.URL, "127.0.0.1", "localhost", 1)+"/admin", http.StatusFound)
		}))
		defer public.Close()

		page, err := Fetch(ctx, FetchClient, public.URL)

		if err == nil || !strings.Contains(err.Error(), "fetch that URL to follow it") || strings.Contains(page.Markdown, "secret") {
			t.Errorf("page = %+v err = %v", page, err)
		}
	})

	t.Run("WB-13 a redirect within the same host is followed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/old" {
				http.Redirect(w, r, "/new", http.StatusMovedPermanently)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "moved here")
		}))
		defer server.Close()

		page, err := Fetch(ctx, FetchClient, server.URL+"/old")

		if err != nil || page.Markdown != "moved here" || !strings.HasSuffix(page.URL, "/new") {
			t.Errorf("page = %+v err = %v", page, err)
		}
	})

	t.Run("WB-14 the search client never follows a redirect", func(t *testing.T) {
		elsewhere := 0
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere++ }))
		defer target.Close()
		engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/search?q=leaked", http.StatusSeeOther)
		}))
		defer engine.Close()

		_, err := (&duckDuckGo{client: SearchClient, baseURL: engine.URL}).Search(ctx, "!w secret")

		if err == nil || elsewhere != 0 {
			t.Errorf("err = %v requests elsewhere = %d", err, elsewhere)
		}
	})

	t.Run("WB-15 XML, YAML and JavaScript come back as text", func(t *testing.T) {
		for _, contentType := range []string{"application/xml", "application/rss+xml", "application/yaml", "application/javascript"} {
			server := serve(t, contentType, "<a>1</a>")
			if page, err := Fetch(ctx, FetchClient, server.URL); err != nil || page.Markdown != "<a>1</a>" {
				t.Errorf("%s: page = %+v err = %v", contentType, page, err)
			}
		}
	})

	t.Run("WB-16 HTML nested too deep to parse still gives its text", func(t *testing.T) {
		deep := "<html><body>" + strings.Repeat("<div>", 600) + "deep words" + strings.Repeat("</div>", 600) + "</body></html>"
		server := serve(t, "text/html", deep)

		page, err := Fetch(ctx, FetchClient, server.URL)

		if err != nil || strings.TrimSpace(page.Markdown) != "deep words" {
			t.Errorf("page = %q err = %v", page.Markdown, err)
		}
	})
}
