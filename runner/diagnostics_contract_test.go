package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
)

func TestExecuteStructuredDiagnosticPreservesCauseAndSource(t *testing.T) {
	t.Parallel()

	rootCause := errors.New("selector backend rejected input")
	fail := action.New("diagnostics_test.fail", func(context.Context, any) (any, error) {
		return nil, xerr.BadRequest("invalid selector", rootCause)
	}).Build()
	bundles := append(native.Bundles(), compiler.Bundle{
		ID:        "diagnostics_test",
		Libraries: []action.Library{{Name: "diagnostics_test", Actions: []action.AnyAction{fail}}},
	})
	cfg, err := BuildConfig(bundles)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	name := filepath.Join(t.TempDir(), "diagnostic.nflow")
	source := "# context\n{} -> diagnostics_test.fail\n"
	if err := os.WriteFile(name, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Execute(t.Context(), cfg, source, name, nil)
	if err == nil {
		t.Fatal("Execute succeeded, want failure")
	}
	if !errors.Is(err, rootCause) {
		t.Fatalf("errors.Is lost the original cause: %v", err)
	}
	var appErr *xerr.AppError
	if !errors.As(err, &appErr) || appErr == nil || appErr.Kind != xerr.KindBadRequest {
		t.Fatalf("errors.As did not retain the typed cause: %#v", appErr)
	}

	err = WithDiagnosticHint(err, "Put exclude patterns before the profile.")
	diagnostic, ok := DiagnosticOf(err)
	if !ok {
		t.Fatal("error does not expose a Flow diagnostic")
	}
	if diagnostic.Code != "BadRequest" || diagnostic.Message != "invalid selector" || diagnostic.Stage != "run" {
		t.Fatalf("diagnostic = %#v, want BadRequest / invalid selector / run", diagnostic)
	}
	if diagnostic.Position.File != name || diagnostic.Position.Line != 2 {
		t.Fatalf("position = %#v, want %s:2", diagnostic.Position, name)
	}
	if diagnostic.Hint != "Put exclude patterns before the profile." {
		t.Fatalf("hint = %q", diagnostic.Hint)
	}
	foundFailedAction := false
	for _, frame := range diagnostic.Frames {
		if frame.Action == "diagnostics_test.fail" {
			foundFailedAction = true
			break
		}
	}
	if !foundFailedAction {
		t.Fatalf("frames = %#v, want diagnostics_test.fail", diagnostic.Frames)
	}

	detail := FormatErrorDetail(err, source, name, false)
	for _, want := range []string{"BadRequest: invalid selector", "stage: run", name + ":2", "2 │ {} -> diagnostics_test.fail", "Put exclude patterns before the profile."} {
		if !strings.Contains(detail, want) {
			t.Errorf("diagnostic detail missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "execution failed") {
		t.Errorf("concise detail retained repeated wrapper noise:\n%s", detail)
	}
	trace := FormatErrorTrace(err)
	if !strings.Contains(trace, "call frames") || !strings.Contains(trace, "diagnostics_test.fail") {
		t.Errorf("verbose trace omitted the call frame:\n%s", trace)
	}
	if !errors.Is(err, rootCause) || !errors.As(err, &appErr) {
		t.Fatal("adding a diagnostic hint changed the original error chain")
	}
}

func TestDiagnosticFramesDeduplicateRepeatedPipelineWrappers(t *testing.T) {
	t.Parallel()

	rootCause := errors.New("root cause")
	err := fmt.Errorf("action pipeline.prepare execution failed: %w", rootCause)
	err = fmt.Errorf("action schema.wrap execution failed: %w", err)
	err = fmt.Errorf("action pipeline.prepare execution failed: %w", err)
	structured := newDiagnosticError(err, "run")
	diagnostic, ok := DiagnosticOf(structured)
	if !ok {
		t.Fatal("error does not expose a Flow diagnostic")
	}
	if len(diagnostic.Frames) != 2 || diagnostic.Frames[0].Action != "pipeline.prepare" || diagnostic.Frames[1].Action != "schema.wrap" {
		t.Fatalf("frames = %#v, want pipeline.prepare once and schema.wrap once", diagnostic.Frames)
	}
	if !errors.Is(structured, rootCause) {
		t.Fatal("structured error lost its original cause")
	}
}

type testDiagnosticSupplementError struct {
	cause error
	hint  string
	stage string
}

func (e *testDiagnosticSupplementError) Error() string               { return e.cause.Error() }
func (e *testDiagnosticSupplementError) Unwrap() error               { return e.cause }
func (e *testDiagnosticSupplementError) FlowDiagnosticHint() string  { return e.hint }
func (e *testDiagnosticSupplementError) FlowDiagnosticStage() string { return e.stage }

func TestDiagnosticOfMergesGenericHostSupplement(t *testing.T) {
	t.Parallel()

	rootCause := errors.New("profile lookup cause")
	err := &testDiagnosticSupplementError{
		cause: xerr.BadRequest("unknown profile", rootCause),
		hint:  "Use the supported profile name.",
		stage: "run",
	}
	diagnostic, ok := DiagnosticOf(err)
	if !ok {
		t.Fatal("generic host supplement was not recognized")
	}
	if diagnostic.Code != "BadRequest" || diagnostic.Message != "unknown profile" || diagnostic.Stage != "run" || diagnostic.Hint != "Use the supported profile name." {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
	if !errors.Is(err, rootCause) {
		t.Fatal("generic host supplement changed the original cause chain")
	}
	formatted := FormatErrorDetail(err, "", "", false)
	if !strings.Contains(formatted, "stage: run") || !strings.Contains(formatted, "Use the supported profile name.") {
		t.Fatalf("generic supplement missing from formatted diagnostic: %s", formatted)
	}
}

func TestDiagnosticFramesTraverseJoinedCauses(t *testing.T) {
	t.Parallel()

	primary := fmt.Errorf("action pipeline.primary execution failed: %w", errors.New("primary failure"))
	cleanup := fmt.Errorf("action cleanup.close execution failed: %w", errors.New("cleanup failure"))
	diagnostic := newDiagnosticError(errors.Join(primary, cleanup), "run").FlowDiagnostic()
	if len(diagnostic.Frames) != 2 || diagnostic.Frames[0].Action != "pipeline.primary" || diagnostic.Frames[1].Action != "cleanup.close" {
		t.Fatalf("frames = %#v, want primary and cleanup call frames", diagnostic.Frames)
	}
}
