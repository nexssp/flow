package syntax_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	exprfile "github.com/expr-lang/expr/file"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	syntaxext "github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestIfCompileErrorFixtures(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	fixtures := syntaxext.Bundle(nil).Fixtures
	for _, name := range []string{
		"if_invalid_syntax.nflow",
		"if_non_bool_condition.nflow",
		"if_invalid_condition_character.nflow",
		"if_empty_condition.nflow",
		"if_bare_keyword.nflow",
		"if_duplicate_else.nflow",
		"if_missing_else_block.nflow",
	} {
		t.Run(name, func(t *testing.T) {
			fixture, err := fs.ReadFile(fixtures, "nflows/"+name)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			want := expectedIfCompileError(t, string(fixture))
			_, err = runner.Compile(t.Context(), cfg, string(fixture), name)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("compile error = %v, want diagnostic containing %q", err, want)
			}
		})
	}
}

func TestIfStaticNonBooleanConditionIsCompileSourceError(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	const name = "if-static-condition.nflow"
	_, err = runner.Compile(t.Context(), cfg, "\nif (42) { noop }", name)
	if err == nil || !strings.Contains(err.Error(), "if: condition must be bool") {
		t.Fatalf("compile error = %v, want static non-boolean diagnostic", err)
	}
	if got := xerr.KindFrom(err); got == xerr.KindValidation {
		t.Fatalf("compile-time condition error has runtime validation kind: %v", err)
	}
	want := compiler.Position{File: name, Line: 2, Col: 5}
	if got, ok := compiler.PositionOf(err); !ok || got != want {
		t.Fatalf("compile error position = %v, %t; want %v", got, ok, want)
	}
}

func TestIfDynamicNonBooleanConditionReturnsValidation(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	_, err = runner.Execute(t.Context(), cfg,
		`if (input.condition) { "yes" } else { "no" }`,
		"if-dynamic-condition.nflow", map[string]any{"condition": "not bool"})
	if err == nil {
		t.Fatal("dynamic non-boolean condition succeeded; want validation error")
	}
	if got := xerr.KindFrom(err); got != xerr.KindValidation {
		t.Fatalf("error kind = %q, want %q: %v", got, xerr.KindValidation, err)
	}
	if !strings.Contains(err.Error(), "if: condition must return bool") {
		t.Fatalf("error = %v, want strict boolean condition diagnostic", err)
	}
}

func TestIfConditionEvaluationErrorsReturnValidationAndPreserveCause(t *testing.T) {
	t.Parallel()

	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}
	_, err = runner.Execute(t.Context(), cfg,
		`if (input.value > 0) { "yes" } else { fail ( message: "condition error became false" ) }`,
		"if-condition-error.nflow", map[string]any{"value": "not a number"})
	if err == nil {
		t.Fatal("condition evaluation error was treated as false")
	}
	if strings.Contains(err.Error(), "condition error became false") {
		t.Fatalf("condition error was swallowed and else branch invoked: %v", err)
	}
	if got := xerr.KindFrom(err); got != xerr.KindValidation {
		t.Fatalf("error kind = %q, want %q: %v", got, xerr.KindValidation, err)
	}
	validationErr, ok := errors.AsType[*xerr.AppError](err)
	if !ok || validationErr.Cause == nil {
		t.Fatalf("error = %v, want validation error with the evaluation cause attached", err)
	}
	if _, ok := errors.AsType[*exprfile.Error](validationErr.Cause); !ok {
		t.Fatalf("validation cause = %v, want original expression evaluation error", validationErr.Cause)
	}
}

func expectedIfCompileError(t *testing.T, source string) string {
	t.Helper()
	const prefix = "# @expect-compile-error "
	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if diagnostic, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(diagnostic)
		}
	}
	t.Fatal("fixture is missing its @expect-compile-error diagnostic")
	return ""
}
