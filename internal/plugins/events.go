package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	lua "github.com/yuin/gopher-lua"

	"github.com/jg/mo/internal/llm"
)

// AfterToolCall passes what a call gave through every tool_result hook in
// turn: a hook that returns text replaces it, as the model will read it; one
// that returns nothing or fails leaves it.
func (s *Set) AfterToolCall(ctx context.Context, call llm.ToolCall, output string, isError bool) string {
	var args any
	json.Unmarshal(call.Args, &args)
	for _, plugin := range s.loaded() {
		for _, hook := range plugin.hooks[hookToolResult] {
			result, err := plugin.callFunction(ctx, hookTimeout, hook, optionalText, func(state *lua.LState) lua.LValue {
				event := state.NewTable()
				event.RawSetString("name", lua.LString(call.Name))
				event.RawSetString("args", toLua(state, args))
				event.RawSetString("output", lua.LString(output))
				event.RawSetString("is_error", lua.LBool(isError))
				return event
			})
			text, _ := result.(string)
			switch {
			case err != nil:
				plugin.log("tool_result hook:", err.Error())
			case len(text) > maxPromptBytes:
				plugin.log(fmt.Sprintf("tool_result hook: result of %d bytes ignored", len(text)))
			case text != "":
				output = text
			}
		}
	}
	return output
}

// EndTurn tells every turn_end hook how a turn ended: its prompt, the
// model's last answer, the error, and whether the user stopped it. What
// they return is not used. They run even when the turn was stopped.
func (s *Set) EndTurn(prompt, answer string, err error) {
	for _, plugin := range s.loaded() {
		for _, hook := range plugin.hooks[hookTurnEnd] {
			_, hookErr := plugin.callFunction(context.Background(), hookTimeout, hook, optionalText, func(state *lua.LState) lua.LValue {
				event := state.NewTable()
				event.RawSetString("prompt", lua.LString(prompt))
				event.RawSetString("answer", lua.LString(answer))
				event.RawSetString("stopped", lua.LBool(errors.Is(err, context.Canceled)))
				if err != nil {
					event.RawSetString("error", lua.LString(err.Error()))
				}
				return event
			})
			if hookErr != nil {
				plugin.log("turn_end hook:", hookErr.Error())
			}
		}
	}
}
