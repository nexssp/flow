package syntax_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/syntax"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func executeFlow(t *testing.T, source string) (runner.Execution, error) {
	t.Helper()
	cfg, err := runner.BuildConfig(native.Bundles())
	if err != nil {
		t.Fatalf("BuildConfig(native.Bundles()): %v", err)
	}
	return runner.Execute(context.Background(), cfg, source, "named_parallel_test.nflow", nil)
}

func parseSyntax(t *testing.T, source, file string) (core.Expr, error) {
	t.Helper()
	bundle := syntax.Bundle(nil)
	parser := core.NewParserWithFile(
		context.Background(),
		core.NewOperatorTable(bundle.Operators...),
		core.NewPrimaryExtensionTable(bundle.Primaries...),
		source,
		file,
	)
	return parser.Parse()
}

func requireSourceError(t *testing.T, err error, location, message string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, location) || !strings.Contains(got, message) {
		t.Fatalf("error = %q, want location %q and message %q", got, location, message)
	}
}

func TestNamedParallel_HappyPath(t *testing.T) {
	execution, err := executeFlow(t, `parallel {
  profile: runtime.const @{ value: "profile-ready" },
  metrics: runtime.const @{ value: "metrics-ready" }
}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := map[string]any{
		"profile": "profile-ready",
		"metrics": "metrics-ready",
	}
	if !reflect.DeepEqual(execution.Output, want) {
		t.Fatalf("output = %#v, want exactly %#v", execution.Output, want)
	}
}

func TestNamedParallel_DuplicateLabel(t *testing.T) {
	_, err := parseSyntax(t, "parallel {\n  worker: noop,\n  worker: noop,\n}\n", "named_parallel.nflow")
	requireSourceError(t, err, "named_parallel.nflow:3:", `duplicate parallel branch label "worker"`)
}

func TestNamedParallel_EmptyBlock(t *testing.T) {
	_, err := parseSyntax(t, "parallel {\n}\n", "named_parallel.nflow")
	requireSourceError(t, err, "named_parallel.nflow:2:", "parallel block must contain at least one branch")
}

func TestNamedParallel_TrailingComma(t *testing.T) {
	execution, err := executeFlow(t, `parallel { first: runtime.const @{ value: "one" }, }`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := map[string]any{"first": "one"}
	if !reflect.DeepEqual(execution.Output, want) {
		t.Fatalf("output = %#v, want %#v", execution.Output, want)
	}
}

func TestNamedParallel_MissingComma(t *testing.T) {
	source := "parallel {\n  first: noop\n  second: noop\n}\n"
	_, err := parseSyntax(t, source, "named_parallel.nflow")
	requireSourceError(t, err, "named_parallel.nflow:3:", "expected ',' or '}'")
}

func TestNamedParallel_NestedAmpersand(t *testing.T) {
	source := `parallel {
  combined: (runtime.const @{ value: "left" } & runtime.const @{ value: "right" }),
  other: runtime.const @{ value: "single" }
}`
	execution, err := executeFlow(t, source)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	output, ok := execution.Output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any", execution.Output)
	}
	if got := output["other"]; got != "single" {
		t.Fatalf("other = %#v, want %q", got, "single")
	}
	inner, ok := output["combined"].(map[string]any)
	if !ok {
		t.Fatalf("combined type = %T, want map[string]any", output["combined"])
	}
	if len(inner) != 2 || inner["runtime.const#1"] != "left" || inner["runtime.const#2"] != "right" {
		t.Fatalf("combined = %#v, want legacy parallel result with both values", inner)
	}
}

func TestNamedParallel_LegacyGroupingAndParallelRemainSupported(t *testing.T) {
	t.Run("parenthesized grouping", func(t *testing.T) {
		execution, err := executeFlow(t, `(runtime.const @{ value: "grouped" })`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if execution.Output != "grouped" {
			t.Fatalf("output = %#v, want %q", execution.Output, "grouped")
		}
	})

	t.Run("old ampersand form", func(t *testing.T) {
		execution, err := executeFlow(t, `(runtime.const @{ value: "left" } & runtime.const @{ value: "right" })`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		want := map[string]any{
			"runtime.const#1": "left",
			"runtime.const#2": "right",
		}
		if !reflect.DeepEqual(execution.Output, want) {
			t.Fatalf("output = %#v, want %#v", execution.Output, want)
		}
	})
}

func TestNamedParallel_PartialResultsDiscardedByFlowComposition(t *testing.T) {
	success := action.New("success", func(context.Context, any) (any, error) {
		return "partial-success", nil
	}).Build()
	failure := action.New("failure", func(context.Context, any) (any, error) {
		return nil, errors.New("branch failed")
	}).Build()
	named := action.ParallelNamed[any]("parallel", map[string]action.AnyAction{
		"good": success,
		"bad":  failure,
	}).Build()

	partial, err := named.Do(context.Background(), nil)
	if err == nil {
		t.Fatal("ParallelNamed.Do error = nil, want branch error")
	}
	if got := partial["good"]; got != "partial-success" {
		t.Fatalf("Kernel partial result = %#v, want successful branch result", partial)
	}

	passthrough := action.New("passthrough", func(_ context.Context, input any) (any, error) {
		return input, nil
	}).Build()
	piped := action.PipeAny("pipe", named, passthrough).Build()
	pipeOutput, pipeErr := piped.Do(context.Background(), nil)
	if pipeErr == nil {
		t.Fatal("PipeAny.Do error = nil, want propagated branch error")
	}
	if pipeOutput != nil {
		t.Fatalf("PipeAny output = %#v, want discarded partial result (nil)", pipeOutput)
	}

	recovered := action.New("recovered", func(context.Context, any) (any, error) {
		return "fallback-result", nil
	}).Build()
	fallback := action.FirstSuccessAny("fallback", named, recovered).Build()
	fallbackOutput, fallbackErr := fallback.Do(context.Background(), nil)
	if fallbackErr != nil {
		t.Fatalf("FirstSuccessAny.Do: %v", fallbackErr)
	}
	if fallbackOutput != "fallback-result" {
		t.Fatalf("fallback output = %#v, want fallback result without partial parallel map", fallbackOutput)
	}
}

func TestNamedParallel_SyntaxIsWiredThroughNativeBundles(t *testing.T) {
	var foundSyntax bool
	var foundKeyword bool
	for _, bundle := range native.Bundles() {
		if bundle.ID != syntax.ID {
			continue
		}
		foundSyntax = true
		for _, primary := range bundle.Primaries {
			if keyword, ok := primary.(core.KeywordPrimary); ok && keyword.Keyword() == "parallel" {
				foundKeyword = true
			}
		}
	}
	if !foundSyntax {
		t.Fatal("native.Bundles() does not include the syntax bundle")
	}
	if !foundKeyword {
		t.Fatal("native syntax bundle does not register the parallel keyword primary")
	}
}
