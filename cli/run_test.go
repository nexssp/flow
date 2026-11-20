package cli

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
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
