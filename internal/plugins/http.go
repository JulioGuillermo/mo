package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/netx"
)

const (
	httpTimeout  = 30 * time.Second
	httpMaxBody  = 32 << 20
	jsonMimeType = "application/json"
)

// httpTable is mo.http: request, get and post.
func (p *Plugin) httpTable() *lua.LTable {
	table := p.state.NewTable()
	table.RawSetString("request", p.state.NewFunction(func(state *lua.LState) int {
		return doRequest(state, state.CheckTable(1))
	}))
	table.RawSetString("get", p.state.NewFunction(func(state *lua.LState) int {
		options := optionsTable(state, 2)
		options.RawSetString("method", lua.LString("GET"))
		options.RawSetString("url", lua.LString(state.CheckString(1)))
		return doRequest(state, options)
	}))
	table.RawSetString("post", p.state.NewFunction(func(state *lua.LState) int {
		options := optionsTable(state, 3)
		options.RawSetString("method", lua.LString("POST"))
		options.RawSetString("url", lua.LString(state.CheckString(1)))
		options.RawSetString("body", lua.LString(state.OptString(2, "")))
		return doRequest(state, options)
	}))
	return table
}

func optionsTable(state *lua.LState, index int) *lua.LTable {
	given := state.OptTable(index, nil)
	options := state.NewTable()
	if given != nil {
		given.ForEach(func(key, value lua.LValue) { options.RawSet(key, value) })
	}
	return options
}

// doRequest sends the request the options describe: url, method, headers,
// body or json (a table, sent as JSON), timeout in seconds, ca and insecure
// as for an MCP server, and redirects = false to stop at the first answer.
// It gives {status, ok, headers, body}, or nil and the error. A status of
// 400 and over is an answer, not an error.
func doRequest(state *lua.LState, options *lua.LTable) int {
	response, err := send(state.Context(), state, options)
	if err != nil {
		state.Push(lua.LNil)
		state.Push(lua.LString(err.Error()))
		return 2
	}
	state.Push(response)
	return 1
}

func send(ctx context.Context, state *lua.LState, options *lua.LTable) (*lua.LTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	body, contentType, err := requestBody(options)
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(luaString(options.RawGetString("method")))
	if method == "" {
		method = http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
	}
	timeout := httpTimeout
	if seconds, ok := options.RawGetString("timeout").(lua.LNumber); ok && seconds > 0 {
		timeout = time.Duration(float64(seconds) * float64(time.Second))
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, luaString(options.RawGetString("url")), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if headers, ok := options.RawGetString("headers").(*lua.LTable); ok {
		headers.ForEach(func(name, value lua.LValue) { request.Header.Set(name.String(), value.String()) })
	}
	transport, err := netx.Transport(luaString(options.RawGetString("ca")), lua.LVAsBool(options.RawGetString("insecure")))
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: transport}
	if options.RawGetString("redirects") == lua.LFalse {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, httpMaxBody))
	if err != nil {
		return nil, err
	}
	return responseTable(state, response, data), nil
}

// requestBody is the body of the options: json as an encoded table, or body
// as it is.
func requestBody(options *lua.LTable) (io.Reader, string, error) {
	if value := options.RawGetString("json"); value != lua.LNil {
		plain, err := toGo(value)
		if err != nil {
			return nil, "", err
		}
		encoded, err := json.Marshal(plain)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(encoded), jsonMimeType, nil
	}
	if body := luaString(options.RawGetString("body")); body != "" {
		return strings.NewReader(body), "", nil
	}
	return nil, "", nil
}

func responseTable(state *lua.LState, response *http.Response, body []byte) *lua.LTable {
	headers := state.NewTable()
	for name, values := range response.Header {
		headers.RawSetString(strings.ToLower(name), lua.LString(strings.Join(values, ", ")))
	}
	table := state.NewTable()
	table.RawSetString("status", lua.LNumber(response.StatusCode))
	table.RawSetString("ok", lua.LBool(response.StatusCode < 400))
	table.RawSetString("headers", headers)
	table.RawSetString("body", lua.LString(body))
	return table
}
