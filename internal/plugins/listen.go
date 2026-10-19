package plugins

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const listenDefaultWait = 2 * time.Minute

// seenRequest is the one request a listener took, as plain strings.
type seenRequest struct {
	method, path, body string
	query              map[string]string
}

// httpListen is mo.http.listen{ port, path, reply }: it listens on this
// computer only, for one request to path (any when empty), such as the
// browser's redirect that ends a login. It gives { port, wait, close }; wait
// takes the seconds to wait and gives { method, path, query, body }, or nil
// and the reason, and closes the listener; reply is the page the browser gets.
func (p *Plugin) httpListen(state *lua.LState) int {
	options := state.OptTable(1, state.NewTable())
	port := 0
	if number, ok := options.RawGetString("port").(lua.LNumber); ok {
		port = int(number)
	}
	path := luaString(options.RawGetString("path"))
	reply := luaString(options.RawGetString("reply"))
	if reply == "" {
		reply = "<!doctype html><title>mo</title><p>Done: you can close this tab and go back to mo."
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return failed(state, err)
	}
	seen := make(chan seenRequest, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path != "" && r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		query := map[string]string{}
		for name, values := range r.URL.Query() {
			query[name] = values[0]
		}
		select {
		case seen <- seenRequest{method: r.Method, path: r.URL.Path, query: query, body: string(body)}:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, reply)
	})}
	go server.Serve(listener)
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}

	table := state.NewTable()
	table.RawSetString("port", lua.LNumber(listener.Addr().(*net.TCPAddr).Port))
	table.RawSetString("close", state.NewFunction(func(*lua.LState) int {
		shutdown()
		return 0
	}))
	table.RawSetString("wait", state.NewFunction(func(state *lua.LState) int {
		defer shutdown()
		wait := time.Duration(float64(state.OptNumber(1, lua.LNumber(listenDefaultWait.Seconds()))) * float64(time.Second))
		ctx := state.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case got := <-seen:
			result := state.NewTable()
			result.RawSetString("method", lua.LString(got.method))
			result.RawSetString("path", lua.LString(got.path))
			result.RawSetString("body", lua.LString(got.body))
			query := state.NewTable()
			for name, value := range got.query {
				query.RawSetString(name, lua.LString(value))
			}
			result.RawSetString("query", query)
			state.Push(result)
			return 1
		case <-time.After(wait):
			return failed(state, context.DeadlineExceeded)
		case <-ctx.Done():
			return failed(state, ctx.Err())
		}
	}))
	state.Push(table)
	return 1
}
