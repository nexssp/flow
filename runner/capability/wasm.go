package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

type wasmModule struct {
	name string
	path string
	r    wazero.Runtime
	mod  wazero.CompiledModule

	closeOnce sync.Once
	closeErr  error
}

// loadWASM now takes ctx so callers can cancel module compilation.
func loadWASM(ctx context.Context, name, path string) (*wasmModule, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("capability %s: read wasm %q: %w", name, path, err)
	}

	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	mod, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = r.Close(ctx)

		return nil, fmt.Errorf("capability %s: compile wasm %q: %w", name, path, err)
	}

	return &wasmModule{name: name, path: path, r: r, mod: mod}, nil
}

// Close stays context-free: cleanup must not inherit a cancelled caller.
func (m *wasmModule) Close() error {
	m.closeOnce.Do(func() {
		m.closeErr = m.r.Close(context.Background())
	})

	return m.closeErr
}

// wasmProxy takes ctx and forwards it to loadWASM.
func wasmProxy(ctx context.Context, b Binding) (action.AnyAction, func() error, error) {
	mod, err := loadWASM(ctx, b.Name, b.Target)
	if err != nil {
		return nil, nil, err
	}

	timeout := parseTimeout(b.Timeout, 60*time.Second)

	act := action.New(b.Name, func(runCtx context.Context, req any) (any, error) {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, xerr.Internal("marshal request failed", err)
		}

		runCtx, cancel := context.WithTimeout(runCtx, timeout)
		defer cancel()

		var stdout, stderr bytes.Buffer

		cfg := wazero.NewModuleConfig().
			WithStdin(bytes.NewReader(body)).
			WithStdout(&stdout).
			WithStderr(&stderr).
			WithName("")

		_, err = mod.r.InstantiateModule(runCtx, mod.mod, cfg)
		if err != nil {
			if exit, ok := err.(*sys.ExitError); ok && exit.ExitCode() != 0 {
				return nil, xerr.Internal(fmt.Sprintf("wasm exit %d; stderr=%s",
					exit.ExitCode(), stderr.String()))
			}

			return nil, xerr.Internal(fmt.Sprintf("wasm run failed: %v; stderr=%s",
				err, stderr.String()))
		}

		var out any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			return nil, xerr.Internal(fmt.Sprintf("decode wasm stdout failed: %v; stdout=%s",
				err, stdout.String()))
		}

		return out, nil
	}).
		Description("wasm capability: "+b.Target).
		Tag("capability", "wasm").
		Build()

	return act, mod.Close, nil
}
