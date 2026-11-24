package external

import (
	"bytes"
	"container/list"
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
	path     string
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	active   int
	retired  bool
	lru      *list.Element
}

// wasmCacheMaxEntries bounds retained runtimes. Active entries evicted from the
// lookup cache are closed only after their last module invocation releases them.
const wasmCacheMaxEntries = 32

var (
	wasmMu    sync.Mutex
	wasmCache = make(map[string]*wasmEntry)
	wasmLRU   list.List
)

// WASM executes a WebAssembly module compiled with WASI. The first
// call compiles the module and caches it; every call instantiates a
// fresh instance so module state does not leak between invocations.
var WASM = action.New("external.wasm", func(ctx context.Context, in map[string]any) (ExecResult, error) {
	path := readStringField(in, "path")
	if path == "" {
		return ExecResult{}, xerr.BadRequest("wasm: 'path' parameter is required")
	}

	entry, err := acquireWASM(ctx, path)
	if err != nil {
		return ExecResult{}, err
	}
	defer releaseWASM(ctx, entry)

	var stdin []byte
	if input, ok := in["input"]; ok && input != nil {
		stdin = inputBytes(input)
	}

	var stdout, stderr outputCapture
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
		Stdout: stdout.buffer.String(),
		Stderr: stderr.buffer.String(),
		OK:     runErr == nil,
		Output: decodeJSON(stdout.buffer.String()),
	}

	if runErr == nil {
		if stdout.truncated || stderr.truncated {
			return result, outputTruncatedError("wasm", stdout.truncated, stderr.truncated)
		}
		return result, nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	exitErr := &sys.ExitError{}
	if errors.As(runErr, &exitErr) {
		result.ExitCode = int(exitErr.ExitCode())
		result.OK = false
		truncation := ""
		if stdout.truncated || stderr.truncated {
			truncation = "; " + outputTruncatedError("wasm", stdout.truncated, stderr.truncated).Error()
		}
		return result, xerr.Internal(fmt.Sprintf(
			"wasm %q exited with code %d: %s%s",
			path, result.ExitCode, strings.TrimSpace(result.Stderr), truncation))
	}

	if stdout.truncated || stderr.truncated {
		return result, xerr.Internal(fmt.Sprintf("wasm %q: %v; %v", path, runErr,
			outputTruncatedError("wasm", stdout.truncated, stderr.truncated)))
	}
	return result, xerr.Internal(fmt.Sprintf("wasm %q: %v", path, runErr))
}).
	Description("Execute sandboxed WebAssembly module with stdin/stdout JSON bridging").
	Tag("external", "wasm", "sandbox").
	Build()

func acquireWASM(ctx context.Context, path string) (*wasmEntry, error) {
	wasmMu.Lock()
	if entry, ok := wasmCache[path]; ok {
		entry.active++
		wasmLRU.MoveToFront(entry.lru)
		wasmMu.Unlock()
		return entry, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		wasmMu.Unlock()
		return nil, xerr.NotFound(fmt.Sprintf("wasm: read %q: %v", path, err))
	}

	runtimeConfig := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)
	runtimeInstance := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtimeInstance)

	compiled, err := runtimeInstance.CompileModule(ctx, data)
	if err != nil {
		_ = runtimeInstance.Close(ctx)
		wasmMu.Unlock()
		return nil, xerr.Internal(fmt.Sprintf("wasm: compile %q: %v", path, err))
	}

	entry := &wasmEntry{path: path, runtime: runtimeInstance, compiled: compiled, active: 1}
	entry.lru = wasmLRU.PushFront(entry)
	wasmCache[path] = entry
	var closeNow []*wasmEntry
	for len(wasmCache) > wasmCacheMaxEntries {
		oldest := wasmLRU.Back()
		if oldest == nil {
			break
		}
		old, ok := oldest.Value.(*wasmEntry)
		if !ok {
			wasmLRU.Remove(oldest)
			continue
		}
		delete(wasmCache, old.path)
		wasmLRU.Remove(oldest)
		old.lru = nil
		old.retired = true
		if old.active == 0 {
			closeNow = append(closeNow, old)
		}
	}
	wasmMu.Unlock()
	for _, old := range closeNow {
		_ = old.runtime.Close(context.WithoutCancel(ctx))
	}
	return entry, nil
}

func releaseWASM(ctx context.Context, entry *wasmEntry) {
	wasmMu.Lock()
	entry.active--
	closeNow := entry.retired && entry.active == 0
	wasmMu.Unlock()
	if closeNow {
		_ = entry.runtime.Close(context.WithoutCancel(ctx))
	}
}
