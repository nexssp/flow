package selftest

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/runtime"
	"github.com/nexssp/flow/native"
)

func TestSyntaxOnlyCompilesWithoutExecutingActions(t *testing.T) {
	var calls atomic.Int32
	probe := action.New("probe.count", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return input, nil
	}).Build()
	feature := compiler.SelfTestFeature{
		Name:   "compile-only probe",
		DSL:    "probe.count",
		Source: "probe.nflow",
	}
	bundle := compiler.Bundle{
		ID:        "syntax-only-probe",
		Libraries: []action.Library{{Name: "probe", Actions: []action.AnyAction{probe}}},
		SelfTest: func() []compiler.SelfTestSection {
			return []compiler.SelfTestSection{{Name: "Syntax only", Features: []compiler.SelfTestFeature{feature}}}
		},
	}

	var output bytes.Buffer
	code := runWithBundles(context.Background(), Options{
		Out:        &output,
		Filters:    []string{feature.Name},
		SyntaxOnly: true,
		JSON:       true,
	}, []compiler.Bundle{runtime.Bundle(nil), bundle})
	if code != 0 {
		t.Fatalf("syntax-only self-test exit code = %d; output:\n%s", code, output.String())
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("compiled action invoked %d times; syntax-only path must not execute it", got)
	}

	var report struct {
		Passed   int `json:"passed"`
		Failed   int `json:"failed"`
		Sections []struct {
			Features []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				SyntaxOnly bool   `json:"syntax_only"`
			} `json:"features"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode syntax-only report: %v\n%s", err, output.String())
	}
	if report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("syntax-only results: passed=%d failed=%d\n%s", report.Passed, report.Failed, output.String())
	}
	featureResult := report.Sections[0].Features[0]
	if featureResult.Name != feature.Name || featureResult.Status != "passed" || !featureResult.SyntaxOnly {
		t.Fatalf("syntax-only feature result = %+v; want passed syntax-only result", featureResult)
	}
}

func TestSyntaxOnlySyntaxGapFixturesCompile(t *testing.T) {
	var output bytes.Buffer
	code := runWithBundles(context.Background(), Options{
		Out: &output,
		Filters: []string{
			"Ternary right associativity",
			"Two-arm ternary compiles",
			"Modifiers and ternary colons",
			"Argument values cover scalar",
		},
		SyntaxOnly: true,
		JSON:       true,
	}, native.SelftestBundles())
	if code != 0 {
		t.Fatalf("syntax-gap fixture self-test exit code = %d; output:\n%s", code, output.String())
	}
	var report struct {
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode syntax-gap report: %v\n%s", err, output.String())
	}
	if report.Passed != 4 || report.Failed != 0 {
		t.Fatalf("syntax-gap results: passed=%d failed=%d\n%s", report.Passed, report.Failed, output.String())
	}
}

func TestMarkedFixtureCompilesInsteadOfExecutingInOrdinaryMode(t *testing.T) {
	var calls atomic.Int32
	probe := action.New("probe.syntax_only", func(_ context.Context, input any) (any, error) {
		calls.Add(1)
		return input, nil
	}).Build()
	bundle := compiler.Bundle{
		ID:        "marked-syntax-only-probe",
		Libraries: []action.Library{{Name: "probe", Actions: []action.AnyAction{probe}}},
		SelfTest: func() []compiler.SelfTestSection {
			return []compiler.SelfTestSection{{Name: "Marked", Features: []compiler.SelfTestFeature{{
				Name:       "marked fixture",
				DSL:        "probe.syntax_only",
				Source:     "marked.nflow",
				SyntaxOnly: true,
			}}}}
		},
	}
	var output bytes.Buffer
	code := runWithBundles(context.Background(), Options{
		Out:     &output,
		Filters: []string{"marked fixture"},
		JSON:    true,
	}, []compiler.Bundle{runtime.Bundle(nil), bundle})
	if code != 0 {
		t.Fatalf("marked syntax-only self-test exit code = %d; output:\n%s", code, output.String())
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("marked fixture action invoked %d times; expected compile-only", got)
	}
}
