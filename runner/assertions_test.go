package runner

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	compiler "github.com/nexssp/flow/compiler"
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

func TestRunAssertionsWithPositions_IncludesSourceLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	code := RunAssertionsContextWithPositions(
		t.Context(),
		&buf,
		map[string]any{"level": "high"},
		0,
		nil,
		[]string{`result.level == "medium"`},
		[]compiler.Position{{File: "checks.nflow", Line: 7}},
		0,
	)
	if code != 1 {
		t.Fatalf("RunAssertionsContextWithPositions returned %d, want 1", code)
	}
	if !strings.Contains(buf.String(), `checks.nflow:7: result.level == "medium"`) {
		t.Fatalf("failure output missing assertion source position:\n%s", buf.String())
	}
}

func TestEvaluateAssertionsWithPositions_IncludesSourceLine(t *testing.T) {
	t.Parallel()
	failures := EvaluateAssertionsContextWithPositions(
		t.Context(),
		map[string]any{"level": "high"},
		[]string{`result.level == "medium"`},
		[]compiler.Position{{File: "checks.nflow", Line: 12}},
	)
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1", len(failures))
	}
	if !strings.Contains(failures[0].Error(), "checks.nflow:12:") {
		t.Fatalf("failure missing assertion source position: %v", failures[0])
	}
}

func TestCollectAsserts_PreservesOrderDuplicatesAndPositions(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"asserts": []string{"result.ok == true", "result.ok == true", "result.count > 0"},
		"asserts.positions": []compiler.Position{
			{File: "flow.nflow", Line: 3},
			{File: "flow.nflow", Line: 4},
			{File: "flow.nflow", Line: 5},
		},
	}
	asserts, positions := collectAsserts(meta, []string{"result.extra == true"})
	if want := []string{"result.ok == true", "result.ok == true", "result.count > 0", "result.extra == true"}; !slices.Equal(asserts, want) {
		t.Fatalf("collected assertions = %v, want %v", asserts, want)
	}
	wantPositions := []compiler.Position{
		{File: "flow.nflow", Line: 3},
		{File: "flow.nflow", Line: 4},
		{File: "flow.nflow", Line: 5},
		{},
	}
	if !slices.Equal(positions, wantPositions) {
		t.Fatalf("collected positions = %v, want %v", positions, wantPositions)
	}
}

func TestRunAssertions_UsesLocalSymbols(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	symbols := map[string]compiler.SymbolNamespace{
		"routing": {Name: "routing", Symbols: map[string]any{"expected": "routed"}},
	}
	assertions := []string{`result.status == routing.expected`}
	result := map[string]any{"status": "routed"}

	if code := RunAssertions(&buf, result, 0, nil, assertions, 0, symbols); code != 0 {
		t.Fatalf("RunAssertions returned %d with local symbols: %s", code, buf.String())
	}
	if failures := EvaluateAssertions(result, assertions, symbols); len(failures) != 0 {
		t.Fatalf("EvaluateAssertions failures = %v", failures)
	}
}
