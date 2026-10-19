package plugins

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"

	lua "github.com/yuin/gopher-lua"
)

// openEncodings adds what talking to a service needs: base64, sha256, hmac
// and url escaping.
func openEncodings(state *lua.LState, api *lua.LTable) {
	functions := map[string]lua.LGFunction{
		"base64_encode": func(state *lua.LState) int {
			state.Push(lua.LString(base64.StdEncoding.EncodeToString([]byte(state.CheckString(1)))))
			return 1
		},
		"base64_decode": func(state *lua.LState) int {
			decoded, err := base64.StdEncoding.DecodeString(state.CheckString(1))
			if err != nil {
				state.Push(lua.LNil)
				state.Push(lua.LString(err.Error()))
				return 2
			}
			state.Push(lua.LString(decoded))
			return 1
		},
		"sha256": func(state *lua.LState) int {
			sum := sha256.Sum256([]byte(state.CheckString(1)))
			state.Push(lua.LString(hex.EncodeToString(sum[:])))
			return 1
		},
		"hmac_sha256": func(state *lua.LState) int {
			mac := hmac.New(sha256.New, []byte(state.CheckString(1)))
			mac.Write([]byte(state.CheckString(2)))
			state.Push(lua.LString(hex.EncodeToString(mac.Sum(nil))))
			return 1
		},
		"url_encode": func(state *lua.LState) int {
			state.Push(lua.LString(url.QueryEscape(state.CheckString(1))))
			return 1
		},
	}
	for name, function := range functions {
		api.RawSetString(name, state.NewFunction(function))
	}
}
