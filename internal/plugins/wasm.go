package plugins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
	lua "github.com/yuin/gopher-lua"
)

const (
	// wasmMemoryPages is the most memory a module gets: 64 KiB pages, 256 MiB.
	wasmMemoryPages = 4096
	maxWasmOutput   = 16 << 20
)

// wasmModule is a WebAssembly module a plugin loaded: compiled once, with
// the instance its exports are called on, made at the first call.
type wasmModule struct {
	compiled wazero.CompiledModule
	instance api.Module
}

// wasmRuntime is the plugin's WebAssembly runtime, made at its first module:
// WASI with no files, network or clock beyond the system's, memory bounded,
// and every call stopped when its plugin call's time runs out.
func (p *Plugin) wasmRuntime(ctx context.Context) wazero.Runtime {
	if p.wasm == nil {
		config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(wasmMemoryPages)
		p.wasm = wazero.NewRuntimeWithConfig(context.Background(), config)
		wasi_snapshot_preview1.MustInstantiate(ctx, p.wasm)
	}
	return p.wasm
}

// loadWasm is mo.wasm(path): the module at path, a relative one in the lib
// folder beside the plugins, as { call, run, exports }.
func (p *Plugin) loadWasm(state *lua.LState) int {
	path := state.CheckString(1)
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.libDir, path)
	}
	code, err := os.ReadFile(path)
	if err != nil {
		state.RaiseError("mo.wasm: %v", err)
	}
	ctx := callContext(state)
	compiled, err := p.wasmRuntime(ctx).CompileModule(ctx, code)
	if err != nil {
		state.RaiseError("mo.wasm %s: %v", filepath.Base(path), err)
	}
	module := &wasmModule{compiled: compiled}
	handle := state.NewTable()
	handle.RawSetString("call", state.NewFunction(func(state *lua.LState) int { return p.callWasm(state, module) }))
	handle.RawSetString("run", state.NewFunction(func(state *lua.LState) int { return p.runWasm(state, module) }))
	exports := state.NewTable()
	for name, definition := range compiled.ExportedFunctions() {
		exports.RawSetString(name, lua.LString(signature(definition)))
	}
	handle.RawSetString("exports", exports)
	state.Push(handle)
	return 1
}

// callWasm is module.call(name, ...): an exported function called with
// numbers, giving numbers. The module's memory and globals stay between
// calls, as a library's do, until a call is stopped: the next starts it
// anew.
func (p *Plugin) callWasm(state *lua.LState, module *wasmModule) int {
	name := state.CheckString(1)
	ctx := callContext(state)
	if module.instance == nil || module.instance.IsClosed() {
		config := wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize")
		instance, err := p.wasmRuntime(ctx).InstantiateModule(ctx, module.compiled, config)
		if err != nil {
			state.RaiseError("wasm: %v", err)
		}
		module.instance = instance
	}
	function := module.instance.ExportedFunction(name)
	if function == nil {
		state.RaiseError("wasm: no exported function %s", name)
	}
	definition := function.Definition()
	params := definition.ParamTypes()
	if state.GetTop()-1 != len(params) {
		state.RaiseError("wasm %s takes %d numbers: %s", name, len(params), signature(definition))
	}
	arguments := make([]uint64, len(params))
	for i, kind := range params {
		arguments[i] = encodeNumber(kind, float64(state.CheckNumber(i+2)))
	}
	results, err := function.Call(ctx, arguments...)
	if err != nil {
		state.RaiseError("wasm %s: %v", name, err)
	}
	for i, kind := range definition.ResultTypes() {
		state.Push(lua.LNumber(decodeNumber(kind, results[i])))
	}
	return len(results)
}

// runWasm is module.run(input, { args, env }): the module run as a WASI
// program, a fresh instance each time, input on its stdin; it gives stdout,
// stderr and the exit code.
func (p *Plugin) runWasm(state *lua.LState, module *wasmModule) int {
	input := state.OptString(1, "")
	options := state.OptTable(2, state.NewTable())
	args, err := stringList(options.RawGetString("args"))
	if err != nil {
		state.RaiseError("wasm run: args: %v", err)
	}
	env, err := stringMap(options.RawGetString("env"))
	if err != nil {
		state.RaiseError("wasm run: env: %v", err)
	}
	var stdout, stderr limitedBuffer
	config := wazero.NewModuleConfig().WithName("").WithStdin(strings.NewReader(input)).
		WithStdout(&stdout).WithStderr(&stderr).WithArgs(append([]string{"module"}, args...)...)
	for key, value := range env {
		config = config.WithEnv(key, value)
	}
	ctx := callContext(state)
	instance, err := p.wasmRuntime(ctx).InstantiateModule(ctx, module.compiled, config)
	code := 0
	var exit *sys.ExitError
	switch {
	case errors.As(err, &exit):
		code = int(exit.ExitCode())
	case err != nil:
		state.RaiseError("wasm run: %v", err)
	}
	if instance != nil {
		instance.Close(ctx)
	}
	state.Push(lua.LString(stdout.String()))
	state.Push(lua.LString(stderr.String()))
	state.Push(lua.LNumber(code))
	return 3
}

// closeWasm frees the plugin's modules.
func (p *Plugin) closeWasm() {
	if p.wasm != nil {
		p.wasm.Close(context.Background())
	}
}

// callContext is the context of the plugin call in progress, which bounds
// what a module does.
func callContext(state *lua.LState) context.Context {
	if ctx := state.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func signature(definition api.FunctionDefinition) string {
	names := func(kinds []api.ValueType) string {
		var parts []string
		for _, kind := range kinds {
			parts = append(parts, api.ValueTypeName(kind))
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprintf("(%s) -> (%s)", names(definition.ParamTypes()), names(definition.ResultTypes()))
}

func encodeNumber(kind api.ValueType, value float64) uint64 {
	switch kind {
	case api.ValueTypeI32:
		return api.EncodeI32(int32(value))
	case api.ValueTypeI64:
		return api.EncodeI64(int64(value))
	case api.ValueTypeF32:
		return api.EncodeF32(float32(value))
	}
	return api.EncodeF64(value)
}

func decodeNumber(kind api.ValueType, value uint64) float64 {
	switch kind {
	case api.ValueTypeI32:
		return float64(api.DecodeI32(value))
	case api.ValueTypeI64:
		return float64(int64(value))
	case api.ValueTypeF32:
		return float64(api.DecodeF32(value))
	}
	return api.DecodeF64(value)
}

// limitedBuffer keeps what a module writes up to maxWasmOutput bytes.
type limitedBuffer struct {
	bytes.Buffer
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if room := maxWasmOutput - b.Len(); room > 0 {
		b.Buffer.Write(data[:min(len(data), room)])
	}
	return len(data), nil
}
