package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

// captureStdout swaps os.Stdout for a pipe during fn and returns what
// was written. Required because runSourceInProcess writes directly to
// os.Stdout, and the info path is what we are testing.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	ktest.RequireNoError(t, err)

	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestRunSourceInProcess_InfoWithDirectives(t *testing.T) {
	// Source starts with @description, exactly like every fixture in
	// the tree. Before the fix, the info path parsed the raw source
	// and reported `unexpected token "description"` on line 1.
	src := `@description "test flow"
"hi"
`
	_ = captureStdout(t, func() {
		rc := runSourceInProcess(context.Background(), src, "test.nflow", []string{"--info"})
		ktest.RequireEqual(t, rc, 0)
	})
}

func TestRunSourceInProcess_InfoWithMacros(t *testing.T) {
	// Source declares a macro and uses it. Info must parse cleanly
	// (requires the macro primary) and list the resulting atoms.
	src := `
@macro hi() { "hi" }
@hi()
`
	_ = captureStdout(t, func() {
		rc := runSourceInProcess(context.Background(), src, "test.nflow", []string{"--info"})
		ktest.RequireEqual(t, rc, 0)
	})
}

func TestRunSourceInProcess_InfoWithPipeline(t *testing.T) {
	// A file whose entire body is a @pipeline directive. Clean output
	// is whitespace; info must still succeed.
	src := `
@pipeline p
  "x"
@end
pipeline.p
`
	_ = captureStdout(t, func() {
		rc := runSourceInProcess(context.Background(), src, "test.nflow", []string{"--info"})
		ktest.RequireEqual(t, rc, 0)
	})
}

func TestRunPreparedSourceInjectsConsoleStderrWriter(t *testing.T) {
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatal(err)
	}
	var code int
	_, stderr := captureIO(t, func() {
		code = runPreparedSource(t.Context(), cfg, `console.stderr(message: "cli stderr")`, "stderr.nflow", nil, nil, runner.DurabilityOptions{}, false)
	})
	if code != 0 {
		t.Fatalf("runPreparedSource exit code = %d; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "cli stderr") {
		t.Fatalf("stderr = %q, want console.stderr output", stderr)
	}
}

func TestRunReportsPositionedPreprocessAndInfoErrors(t *testing.T) {
	t.Setenv(harnessEnv, "")
	for _, test := range []struct {
		name    string
		source  string
		flags   []string
		context string
	}{
		{name: "preprocess", source: "# context\n@include\nruntime.noop\n", context: "preprocess/config"},
		{name: "info parse", source: "# context\nruntime.noop ( value: )\n", flags: []string{"--info"}, context: "parse"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "broken.nflow")
			if err := os.WriteFile(path, []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}

			args := append([]string{"run", path}, test.flags...)
			var code int
			stdout, stderr := captureIO(t, func() {
				code = RunWithBundles(args, nil)
			})
			if code == 0 {
				t.Fatalf("run returned success; stdout=%q stderr=%q", stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("diagnostic leaked to stdout: %q", stdout)
			}
			for _, want := range []string{test.context, path + ":2", "2 │", "^"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("diagnostic missing %q: %s", want, stderr)
				}
			}
		})
	}
}

func TestRunSourceInProcess_PreprocessErrorIncludesSourceSnippet(t *testing.T) {
	var code int
	stdout, stderr := captureIO(t, func() {
		code = runSourceInProcess(context.Background(), "# context\n@include\nruntime.noop\n", "inline.nflow", nil)
	})
	if code == 0 {
		t.Fatalf("run returned success; stdout=%q stderr=%q", stdout, stderr)
	}
	for _, want := range []string{"preprocess/config", "inline.nflow:2", "2 │ @include", "^"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("diagnostic missing %q: %s", want, stderr)
		}
	}
}

func TestRunEmbeddedInfoParseErrorIncludesSourceSnippet(t *testing.T) {
	source := "# context\nruntime.noop ( value: )\n"
	var code int
	stdout, stderr := captureIO(t, func() {
		code = RunEmbeddedWithBundles(context.Background(), source, []string{"info"}, nil)
	})
	if code == 0 {
		t.Fatalf("embedded info returned success; stdout=%q stderr=%q", stdout, stderr)
	}
	for _, want := range []string{"parse", "<embedded>:2", "2 │ runtime.noop ( value: )", "^"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("diagnostic missing %q: %s", want, stderr)
		}
	}
}

func TestFatalSourceErrorKeepsUnpositionedErrorConcise(t *testing.T) {
	var code int
	_, stderr := captureIO(t, func() {
		code = fatalSourceError("preprocess", errors.New("opaque error"), "source", "test.nflow")
	})
	ktest.RequireEqual(t, code, 2)
	ktest.RequireStringContains(t, stderr, "❌ preprocess: opaque error")
	ktest.RequireCondition(t, !strings.Contains(stderr, "test.nflow:") && !strings.Contains(stderr, "1 │"), "unpositioned error should not include a source excerpt: %s", stderr)
}

func TestEmitJSONOutputQuotesStrings(t *testing.T) {
	var out bytes.Buffer
	emitJSONOutput(&out, "42")
	ktest.RequireEqual(t, out.String(), "\"42\"\n")
}

func TestEmitCleanOutputPrintsStringsAsPlainText(t *testing.T) {
	var out bytes.Buffer
	emitCleanOutput(&out, "42")
	ktest.RequireEqual(t, out.String(), "42\n")
}

func TestSplitPayloadAndFlagsKeepsPositionalArguments(t *testing.T) {
	payload, flags, err := splitPayloadAndFlags([]string{"42", "--json", "-x=7"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(payload), 0)
	ktest.RequireEqual(t, flags, []string{"42", "--json", "-x=7"})
}

func TestSplitPositionalsAndFlagsKeepsJSONLookingString(t *testing.T) {
	value := `{"a":1}`
	payload, flags, err := splitPositionalsAndFlags([]string{value, "--json"})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual(t, len(payload), 0)
	ktest.RequireEqual(t, flags, []string{value, "--json"})
}

func TestIsInlineSourceDirectValuesAndFilePrecedence(t *testing.T) {
	for _, source := range []string{`"ok"`, `42`, `true`, `[1,2]`, `.user.name`} {
		t.Run(source, func(t *testing.T) {
			if !isInlineSource(context.Background(), source) {
				t.Fatalf("isInlineSource(%q) = false, want true", source)
			}
		})
	}

	file, err := os.OpenFile("42", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		if closeErr := file.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		defer os.Remove("42")
	} else if !os.IsExist(err) {
		t.Fatal(err)
	}
	if isInlineSource(context.Background(), "42") {
		t.Fatal("an existing file named 42 must take precedence over direct-value parsing")
	}
}

func TestIsInlineSourceMissingConstAndNoopPaths(t *testing.T) {
	for _, target := range []string{
		"missing-const.nflow",
		"missing-noop.nflow",
		"missing-const-noop",
	} {
		t.Run(target, func(t *testing.T) {
			if isInlineSource(context.Background(), target) {
				t.Fatalf("isInlineSource(%q) = true, want missing path to remain a file target", target)
			}
		})
	}
}

func TestRenderColoredJSONStartsImmediately(t *testing.T) {
	var out bytes.Buffer
	renderColoredJSON(&out, map[string]any{"tags": []string{"a,b", "green"}}, false)

	want := "{\n  \"tags\": [\n    \"a,b\",\n    \"green\"\n  ]\n}\n"
	ktest.RequireEqual(t, out.String(), want)
}
