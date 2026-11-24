package external

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/tetratelabs/wazero"
)

func TestWASMOutputCaptureLimit(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			var capture outputCapture
			payload := strings.Repeat("x", outputCaptureMaxBytes+1)
			n, err := capture.Write([]byte(payload))
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if n != len(payload) {
				t.Fatalf("Write consumed %d bytes, want %d to keep WASI writes draining", n, len(payload))
			}
			if capture.buffer.Len() != outputCaptureMaxBytes || !capture.truncated {
				t.Fatalf("%s capture len/truncated = %d/%t, want %d/true", stream, capture.buffer.Len(), capture.truncated, outputCaptureMaxBytes)
			}
		})
	}
}

func TestWASMOutputLimitThroughWASIAction(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			fd := int32(1)
			if stream == "stderr" {
				fd = 2
			}
			path := filepath.Join(t.TempDir(), "output.wasm")
			if err := os.WriteFile(path, wasmOutputModule(fd), 0o600); err != nil {
				t.Fatalf("write WASI output fixture: %v", err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			raw, err := action.InvokeAny(ctx, WASM, map[string]any{"path": path})
			result, ok := raw.(ExecResult)
			if !ok {
				t.Fatalf("WASM result type = %T, want ExecResult (error: %v)", raw, err)
			}
			if err == nil || !strings.Contains(err.Error(), stream) || !strings.Contains(err.Error(), "output truncated") {
				t.Fatalf("WASM error = %v, want %s output truncation", err, stream)
			}
			want := strings.Repeat("x", outputCaptureMaxBytes)
			if stream == "stdout" {
				if result.Stdout != want || result.Stderr != "" {
					t.Fatalf("captured stdout/stderr = %d/%d bytes, want %d/%d with partial stdout preserved", len(result.Stdout), len(result.Stderr), len(want), 0)
				}
			} else if result.Stderr != want || result.Stdout != "" {
				t.Fatalf("captured stdout/stderr = %d/%d bytes, want %d/%d with partial stderr preserved", len(result.Stdout), len(result.Stderr), 0, len(want))
			}
		})
	}
}

func TestWASMCancellationReleasesCacheLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.wasm")
	if err := os.WriteFile(path, wasmInfiniteLoopModule(), 0o600); err != nil {
		t.Fatalf("write infinite-loop fixture: %v", err)
	}
	entry, err := acquireWASM(t.Context(), path)
	if err != nil {
		t.Fatalf("precompile infinite-loop fixture: %v", err)
	}
	releaseWASM(t.Context(), entry)
	t.Cleanup(func() {
		wasmMu.Lock()
		if wasmCache[path] == entry {
			delete(wasmCache, path)
		}
		if entry.lru != nil {
			wasmLRU.Remove(entry.lru)
			entry.lru = nil
		}
		entry.retired = true
		closeNow := entry.active == 0
		wasmMu.Unlock()
		if closeNow {
			_ = entry.runtime.Close(context.WithoutCancel(t.Context()))
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct {
		result any
		err    error
	}, 1)
	go func() {
		result, invokeErr := action.InvokeAny(ctx, WASM, map[string]any{"path": path})
		done <- struct {
			result any
			err    error
		}{result: result, err: invokeErr}
	}()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	leaseDeadline := time.NewTimer(5 * time.Second)
	defer leaseDeadline.Stop()
	for {
		wasmMu.Lock()
		active := entry.active
		wasmMu.Unlock()
		if active > 0 {
			break
		}
		select {
		case invocation := <-done:
			t.Fatalf("WASM invocation returned before acquiring a lease: %v", invocation.err)
		case <-ticker.C:
		case <-leaseDeadline.C:
			t.Fatal("WASM invocation did not acquire its cache lease")
		}
	}

	cancel()
	select {
	case invocation := <-done:
		if !errors.Is(invocation.err, context.Canceled) {
			t.Fatalf("WASM cancellation error = %v, want context.Canceled", invocation.err)
		}
		if _, ok := invocation.result.(ExecResult); !ok {
			t.Fatalf("WASM result type = %T, want ExecResult", invocation.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("non-terminating WASM invocation did not return after cancellation")
	}

	wasmMu.Lock()
	active := entry.active
	wasmMu.Unlock()
	if active != 0 {
		t.Fatalf("WASM cache leases after cancellation = %d, want 0", active)
	}
}

func wasmOutputModule(fd int32) []byte {
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	types := []byte{2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0}
	module = wasmAppendSection(module, 1, types)

	imports := []byte{1}
	imports = wasmAppendName(imports, "wasi_snapshot_preview1")
	imports = wasmAppendName(imports, "fd_write")
	imports = append(imports, 0, 0)
	module = wasmAppendSection(module, 2, imports)
	module = wasmAppendSection(module, 3, []byte{1, 1})
	module = wasmAppendSection(module, 5, []byte{1, 0, 17}) // 17 pages, enough for the payload.

	exports := []byte{2}
	exports = wasmAppendName(exports, "_start")
	exports = append(exports, 0, 1)
	exports = wasmAppendName(exports, "memory")
	exports = append(exports, 2, 0)
	module = wasmAppendSection(module, 7, exports)

	code := []byte{0}                     // no locals
	code = wasmAppendI32Const(code, 1024) // memory.fill destination
	code = wasmAppendI32Const(code, 'x')
	code = wasmAppendI32Const(code, outputCaptureMaxBytes+1)
	code = append(code, 0xfc, 0x0b, 0)  // memory.fill memory 0
	code = wasmAppendI32Const(code, 16) // iovec pointer
	code = wasmAppendI32Const(code, 1024)
	code = append(code, 0x36, 2, 0)     // i32.store payload pointer
	code = wasmAppendI32Const(code, 20) // iovec length
	code = wasmAppendI32Const(code, outputCaptureMaxBytes+1)
	code = append(code, 0x36, 2, 0) // i32.store payload length
	code = wasmAppendI32Const(code, fd)
	code = wasmAppendI32Const(code, 16)
	code = wasmAppendI32Const(code, 1)       // one iovec
	code = wasmAppendI32Const(code, 24)      // nwritten pointer
	code = append(code, 0x10, 0, 0x1a, 0x0b) // call fd_write, drop errno, end
	body := wasmAppendSize([]byte{1}, len(code))
	body = append(body, code...)
	module = wasmAppendSection(module, 10, body)
	return module
}

func wasmInfiniteLoopModule() []byte {
	module := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	module = wasmAppendSection(module, 1, []byte{1, 0x60, 0, 0})
	module = wasmAppendSection(module, 3, []byte{1, 0})
	exports := wasmAppendName([]byte{1}, "_start")
	exports = append(exports, 0, 0)
	module = wasmAppendSection(module, 7, exports)
	body := []byte{0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b} // loop { br 0 }
	code := wasmAppendSize([]byte{1}, len(body))
	code = append(code, body...)
	return wasmAppendSection(module, 10, code)
}

func wasmAppendSection(module []byte, id byte, payload []byte) []byte {
	module = append(module, id)
	module = wasmAppendSize(module, len(payload))
	return append(module, payload...)
}

func wasmAppendName(dst []byte, value string) []byte {
	dst = wasmAppendSize(dst, len(value))
	return append(dst, value...)
}

func wasmAppendSize(dst []byte, value int) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value&0x7f)|0x80)
		value >>= 7
	}
	return append(dst, byte(value&0x7f))
}

func wasmAppendI32Const(dst []byte, value int32) []byte {
	dst = append(dst, 0x41)
	for {
		b := byte(value & 0x7f)
		value >>= 7
		more := (value != 0 || b&0x40 != 0) && (value != -1 || b&0x40 == 0)
		if more {
			b |= 0x80
		}
		dst = append(dst, b)
		if !more {
			return dst
		}
	}
}

func TestWASMCacheBoundedAndDoesNotCloseInFlightRuntime(t *testing.T) {
	const wasmEmpty = "\x00asm\x01\x00\x00\x00"
	entries := make([]*wasmEntry, 0, wasmCacheMaxEntries+1)
	released := make([]bool, wasmCacheMaxEntries+1)
	defer func() {
		for i, entry := range entries {
			if !released[i] {
				releaseWASM(t.Context(), entry)
			}
		}
		wasmMu.Lock()
		remaining := make([]*wasmEntry, 0, len(wasmCache))
		for key, entry := range wasmCache {
			delete(wasmCache, key)
			entry.retired = true
			entry.lru = nil
			if entry.active == 0 {
				remaining = append(remaining, entry)
			}
		}
		wasmLRU.Init()
		wasmMu.Unlock()
		for _, entry := range remaining {
			_ = entry.runtime.Close(context.WithoutCancel(t.Context()))
		}
	}()

	for range wasmCacheMaxEntries + 1 {
		path := filepath.Join(t.TempDir(), "empty.wasm")
		if err := os.WriteFile(path, []byte(wasmEmpty), 0o600); err != nil {
			t.Fatalf("write wasm fixture: %v", err)
		}
		entry, err := acquireWASM(t.Context(), path)
		if err != nil {
			t.Fatalf("acquireWASM(%q): %v", path, err)
		}
		entries = append(entries, entry)
	}

	wasmMu.Lock()
	cacheSize := len(wasmCache)
	firstRetired, firstActive := entries[0].retired, entries[0].active
	wasmMu.Unlock()
	if cacheSize > wasmCacheMaxEntries {
		t.Fatalf("WASM cache size = %d, limit %d", cacheSize, wasmCacheMaxEntries)
	}
	if !firstRetired || firstActive != 1 {
		t.Fatalf("evicted in-flight entry retired/active = %t/%d, want true/1", firstRetired, firstActive)
	}

	module, err := entries[0].runtime.InstantiateModule(t.Context(), entries[0].compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		t.Fatalf("evicted in-flight runtime was closed prematurely: %v", err)
	}
	if err := module.Close(t.Context()); err != nil {
		t.Fatalf("close test module: %v", err)
	}

	releaseWASM(t.Context(), entries[0])
	released[0] = true
	if _, err := entries[0].runtime.CompileModule(t.Context(), []byte(wasmEmpty)); err == nil {
		t.Fatal("evicted runtime remains open after its final lease was released")
	}
}
