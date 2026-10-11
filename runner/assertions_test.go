package runner

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunAssertions_FailureEmitsExitStatusTrailer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	code := RunAssertions(
		&buf,
		map[string]any{"level": "high"},
		0, nil,
		[]string{`result.level == "medium"`},
		0,
	)

	if code != 1 {
		t.Fatalf("RunAssertions returned %d, want 1", code)
	}
	out := buf.String()
	if !strings.Contains(out, "exit status: 1") {
		t.Fatalf("output missing exit-status trailer:\n%s", out)
	}
}

func TestRunAssertions_PassDoesNotEmitExitStatusTrailer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	code := RunAssertions(
		&buf,
		map[string]any{"level": "medium"},
		0, nil,
		[]string{`result.level == "medium"`},
		0,
	)

	if code != 0 {
		t.Fatalf("RunAssertions returned %d, want 0", code)
	}
	if strings.Contains(buf.String(), "exit status") {
		t.Fatalf("pass output should not contain an exit-status trailer:\n%s", buf.String())
	}
}

func TestRunAssertions_NonFileWriterStaysPlain(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	RunAssertions(
		&buf,
		map[string]any{"level": "high"},
		0, nil,
		[]string{`result.level == "medium"`},
		0,
	)

	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("non-file writer should not receive ANSI escapes:\n%q", buf.String())
	}
}
