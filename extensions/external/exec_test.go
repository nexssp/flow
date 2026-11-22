package external

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func runExec(tb testing.TB, in map[string]any) (ExecResult, error) {
	tb.Helper()
	raw, err := action.InvokeAny(tb.Context(), Exec, in)
	result, ok := raw.(ExecResult)
	if !ok && raw != nil {
		tb.Fatalf("result type = %T, want ExecResult", raw)
	}
	return result, err
}

func TestExec_MissingCommand(t *testing.T) {
	t.Parallel()
	_, err := runExec(t, map[string]any{})
	ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
}

func TestExec_SuccessfulCommand(t *testing.T) {
	t.Parallel()
	result, err := runExec(t, map[string]any{"cmd": "echo hello"})
	ktest.RequireNoError(t, err)
	ktest.RequireCondition(t, result.OK, "OK should be true")
	ktest.RequireEqual(t, result.ExitCode, 0)
	ktest.RequireStringContains(t, result.Stdout, "hello")
}

func TestExec_ArgvPreservesArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	want := []string{`J:\Temp\nflow lang\typescript.ts`, `single'quote`, `&|<>`}
	args := []string{"-test.run=^TestExecArgvHelper$", "--", "external-exec-argv-helper"}
	args = append(args, want...)
	result, err := runExec(t, map[string]any{"cmd": executable, "args": args})
	ktest.RequireNoError(t, err)

	var got []string
	if err := json.Unmarshal([]byte(result.Stdout), &got); err != nil {
		t.Fatalf("decode child arguments %q: %v", result.Stdout, err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("child arguments = %#v; want %#v", got, want)
	}
}

func TestExecArgvHelper(_ *testing.T) {
	marker := slices.Index(os.Args, "external-exec-argv-helper")
	if marker < 0 {
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(os.Args[marker+1:]); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestExec_CancellationPreservesContextError(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	ready := filepath.Join(t.TempDir(), "child-ready")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, execErr := action.InvokeAny(ctx, Exec, map[string]any{
			"cmd": executable,
			"args": []string{
				"-test.run=^TestExecCancellationHelper$", "--", "external-exec-cancel-helper", ready,
			},
		})
		done <- execErr
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	startDeadline := time.NewTimer(5 * time.Second)
	defer startDeadline.Stop()
	for {
		if _, statErr := os.Stat(ready); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("stat child ready marker: %v", statErr)
		}
		select {
		case execErr := <-done:
			t.Fatalf("child exited before signaling readiness: %v", execErr)
		case <-ticker.C:
		case <-startDeadline.C:
			t.Fatal("child did not signal readiness")
		}
	}

	cancel()
	select {
	case execErr := <-done:
		if !errors.Is(execErr, context.Canceled) {
			t.Fatalf("external exec error = %v, want context.Canceled", execErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("external exec did not return after cancellation")
	}
}

func TestExecCancellationHelper(t *testing.T) {
	marker := slices.Index(os.Args, "external-exec-cancel-helper")
	if marker < 0 {
		return
	}
	if marker+1 >= len(os.Args) {
		t.Fatal("missing child ready marker path")
	}
	// #nosec G703 -- Only the parent test's temporary marker path is passed here.
	if err := os.WriteFile(os.Args[marker+1], []byte("ready"), 0o600); err != nil {
		t.Fatalf("write child ready marker: %v", err)
	}
	select {}
}

func TestExec_NonZeroExit(t *testing.T) {
	t.Parallel()
	result, err := runExec(t, map[string]any{"cmd": "exit 3"})
	ktest.RequireCondition(t, err != nil, "expected error for non-zero exit")
	ktest.RequireEqual(t, result.ExitCode, 3)
	ktest.RequireCondition(t, !result.OK, "OK should be false")
}

func TestExec_JSONStdoutDecoded(t *testing.T) {
	t.Parallel()
	command := `echo '{"name":"Maksymilian","age":42}'`
	if runtime.GOOS == "windows" {
		command = `echo {"name":"Maksymilian","age":42}`
	}
	result, err := runExec(t, map[string]any{"cmd": command})
	ktest.RequireNoError(t, err)

	m, ok := result.Output.(map[string]any)
	ktest.RequireCondition(t, ok, "Output = %T, want map", result.Output)
	ktest.RequireEqual(t, m["name"], "Maksymilian")
}

func TestExec_NonJSONStdout(t *testing.T) {
	t.Parallel()
	result, err := runExec(t, map[string]any{"cmd": "echo plain"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, result.Output, nil)
}

func TestExec_InputViaStdin(t *testing.T) {
	t.Parallel()
	stdinEcho := "cat"
	if runtime.GOOS == "windows" {
		stdinEcho = `findstr /r .`
	}
	result, err := runExec(t, map[string]any{
		"cmd":   stdinEcho,
		"input": "from stdin",
	})
	ktest.RequireNoError(t, err)
	ktest.RequireStringContains(t, result.Stdout, "from stdin")
}

func TestInputBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hi", "hi"},
		{"bytes", []byte("hi"), "hi"},
		{"json map", map[string]any{"a": 1}, `{"a":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, string(inputBytes(c.in)), c.want)
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want any
	}{
		{"object", `{"a":1}`, map[string]any{"a": float64(1)}},
		{"array", `[1,2]`, []any{float64(1), float64(2)}},
		{"plain", "hello", nil},
		{"empty", "", nil},
		{"malformed", `{"a":`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ktest.RequireEqual(t, decodeJSON(c.in), c.want)
		})
	}
}

func TestReadStringField(t *testing.T) {
	t.Parallel()
	in := map[string]any{"cmd": "x", "command": "y"}
	ktest.RequireEqual(t, readStringField(in, "cmd"), "x")
	ktest.RequireEqual(t, readStringField(in, "command", "cmd"), "y")
	ktest.RequireEqual(t, readStringField(in, "missing"), "")
}
