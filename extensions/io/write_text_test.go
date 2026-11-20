package io

import (
	"bytes"
	"context"
	"errors"
	stdio "io"
	stdfs "io/fs"
	"os"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/extensions/description"
	"github.com/nexssp/flow/extensions/projection"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/runner"
)

func TestWriteTextActionWritesRawTextAndPassesInput(t *testing.T) {
	input := map[string]any{"text": "first\nsecond"}
	var got any
	var runErr error
	output := captureProcessStdout(t, func() {
		got, runErr = WriteTextAction.DoAny(context.Background(), input)
	})
	ktest.RequireNoError(t, runErr)
	passedThrough, ok := got.(map[string]any)
	ktest.RequireCondition(t, ok, "WriteTextAction result type = %T, want map[string]any", got)
	ktest.RequireEqual(t, passedThrough, input)
	ktest.RequireEqual(t, output, "first\nsecond\n")
}

func TestWriteTextActionRejectsMissingOrNonStringText(t *testing.T) {
	for _, input := range []map[string]any{
		{},
		{"text": 42},
		{"text": nil},
	} {
		_, err := WriteTextAction.DoAny(context.Background(), input)
		ktest.RequireNotNil(t, err)
		ktest.RequireEqual(t, xerr.KindFrom(err), xerr.KindBadRequest)
	}
}

func TestWriteTextAddsOneFinalNewline(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	ktest.RequireNoError(t, writeText(&output, "already rendered"))
	ktest.RequireEqual(t, output.String(), "already rendered\n")
}

func TestWriteTextDoesNotDuplicateNewlineOrWriteEmptyText(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	ktest.RequireNoError(t, writeText(&output, "first\r\n"))
	ktest.RequireNoError(t, writeText(&output, ""))
	ktest.RequireEqual(t, output.String(), "first\r\n")
}

func TestWriteTextPropagatesWriterErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("closed")
	if err := writeText(errorWriter{err: wantErr}, "text"); !errors.Is(err, wantErr) {
		t.Fatalf("writeText() error = %v, want %v", err, wantErr)
	}
	if err := writeText(shortWriter{}, "text"); !errors.Is(err, stdio.ErrShortWrite) {
		t.Fatalf("writeText() error = %v, want %v", err, stdio.ErrShortWrite)
	}
}

func TestWriteTextInputClassifiesWriterErrors(t *testing.T) {
	t.Parallel()

	_, err := writeTextInput(errorWriter{err: errors.New("closed")}, map[string]any{"text": "text"})
	ktest.RequireNotNil(t, err)
	ktest.RequireEqual(t, xerr.KindFrom(err), xerr.KindInternal)
}

func TestWriteTextFixture(t *testing.T) {
	source, err := stdfs.ReadFile(Bundle(nil).Fixtures, "nflows/write_text.nflow")
	ktest.RequireNoError(t, err)

	cfg, err := runner.BuildConfig([]compiler.Bundle{
		syntax.Bundle(nil),
		description.Bundle(nil),
		runtime.Bundle(nil),
		assert.Bundle(nil),
		projection.Bundle(nil),
		Bundle(nil),
	})
	ktest.RequireNoError(t, err)

	var rendered, stderr bytes.Buffer
	code := -1
	written := captureProcessStdout(t, func() {
		code = runner.RunSource(t.Context(), cfg, string(source), "write_text.nflow", nil, runner.Opts{
			Stdout: &rendered,
			Stderr: &stderr,
		})
	})
	if code != 0 {
		t.Fatalf("RunSource returned %d; direct stdout=%q; rendered stdout=%q; stderr=%q", code, written, rendered.String(), stderr.String())
	}
	ktest.RequireEqual(t, stderr.String(), "")
	ktest.RequireEqual(t, written, "RepoPeek fixture output\n")
}

func captureProcessStdout(t *testing.T, run func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	ktest.RequireNoError(t, err)
	original := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
		_ = writer.Close()
		_ = reader.Close()
	})

	run()
	ktest.RequireNoError(t, writer.Close())
	os.Stdout = original
	output, err := stdio.ReadAll(reader)
	ktest.RequireNoError(t, err)
	ktest.RequireNoError(t, reader.Close())
	return string(output)
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
