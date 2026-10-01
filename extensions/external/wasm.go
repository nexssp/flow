package external

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

type wasmEntry struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
}

var (
	wasmMu    sync.RWMutex
	wasmCache = make(map[string]*wasmEntry)
)

// WASM executes a WebAssembly module compiled with WASI. The first
// call compiles the module and caches it; every call instantiates a
// fresh instance so module state does not leak between invocations.
var WASM = action.New("wasm", func(ctx context.Context, in map[string]any) (ExecResult, error) {
	path := readStringField(in, "path")
	if path == "" {
		return ExecResult{}, xerr.BadRequest("wasm: 'path' parameter is required")
	}

	entry, err := getOrCompileWASM(ctx, path)
	if err != nil {
		return ExecResult{}, err
	}

	var stdin []byte
	if input, ok := in["input"]; ok && input != nil {
		stdin = inputBytes(input)
	}

	var stdout, stderr bytes.Buffer
	moduleConfig := wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(stdin)).
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithName("")

	mod, runErr := entry.runtime.InstantiateModule(ctx, entry.compiled, moduleConfig)
	if mod != nil {
		defer mod.Close(ctx)
	}

	result := ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		OK:     runErr == nil,
		Output: decodeJSON(stdout.String()),
	}

	if runErr == nil {
		return result, nil
	}

	exitErr := &sys.ExitError{}
	if errors.As(runErr, &exitErr) {
		result.ExitCode = int(exitErr.ExitCode())
		result.OK = false
		return result, xerr.Internal(fmt.Sprintf(
			"wasm %q exited with code %d: %s",
			path, result.ExitCode, strings.TrimSpace(result.Stderr)))
	}

	return result, xerr.Internal(fmt.Sprintf("wasm %q: %v", path, runErr))
}).
	Description("Execute sandboxed WebAssembly module with stdin/stdout JSON bridging").
	Tag("external", "wasm", "sandbox").
	Build()

func getOrCompileWASM(ctx context.Context, path string) (*wasmEntry, error) {
	wasmMu.RLock()
	if entry, ok := wasmCache[path]; ok {
		wasmMu.RUnlock()
		return entry, nil
	}
	wasmMu.RUnlock()

	wasmMu.Lock()
	defer wasmMu.Unlock()

	if entry, ok := wasmCache[path]; ok {
		return entry, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, xerr.NotFound(fmt.Sprintf("wasm: read %q: %v", path, err))
	}

	runtimeInstance := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtimeInstance)

	compiled, err := runtimeInstance.CompileModule(ctx, data)
	if err != nil {
		_ = runtimeInstance.Close(ctx)
		return nil, xerr.Internal(fmt.Sprintf("wasm: compile %q: %v", path, err))
	}

	entry := &wasmEntry{runtime: runtimeInstance, compiled: compiled}
	wasmCache[path] = entry
	return entry, nil
}
