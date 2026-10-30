package test

import (
	"context"
	"strings"
	"testing"

	"github.com/nexssp/flow/compiler"
)

func TestBundleIsExplicitAndNotProcessRegistered(t *testing.T) {
	bundle := Bundle(nil)
	if bundle.ID != ID {
		t.Fatalf("bundle ID = %q, want %q", bundle.ID, ID)
	}
	if len(bundle.Directives) != 1 || bundle.Directives[0].Name != "test" {
		t.Fatalf("bundle directives = %#v, want only @test", bundle.Directives)
	}
	if _, registered := compiler.Lookup(ID); registered {
		t.Fatal("@test bundle was registered globally; it must be explicitly mounted")
	}
}

func TestDirectiveRecordsPostRunAssertion(t *testing.T) {
	out, err := runTestDirective(t, `@test: result.value == "ok"`)
	if err != nil {
		t.Fatalf("@test handler error = %v", err)
	}
	assertions, _ := out["asserts"].([]string)
	if len(assertions) != 1 || assertions[0] != `result.value == "ok"` {
		t.Fatalf("assertions = %#v", assertions)
	}
	positions, _ := out["asserts.positions"].([]compiler.Position)
	if len(positions) != 1 || positions[0] != (compiler.Position{File: "fixture.nflow", Line: 1}) {
		t.Fatalf("assertion positions = %#v", positions)
	}
}

func TestDirectiveBlockPreservesExpressionLines(t *testing.T) {
	out, err := runTestDirective(t,
		"@test {",
		"  result.value == \"ok\"",
		"  result.count > 0",
		"}",
	)
	if err != nil {
		t.Fatalf("@test block handler error = %v", err)
	}
	assertions, _ := out["asserts"].([]string)
	if len(assertions) != 2 || assertions[0] != `result.value == "ok"` || assertions[1] != "result.count > 0" {
		t.Fatalf("assertions = %#v", assertions)
	}
	positions, _ := out["asserts.positions"].([]compiler.Position)
	if len(positions) != 2 || positions[0].Line != 2 || positions[1].Line != 3 {
		t.Fatalf("assertion positions = %#v", positions)
	}
}

func TestDirectiveErrorsNameTestAndKeepSourcePosition(t *testing.T) {
	_, err := runTestDirective(t, "@test: result.value >")
	if err == nil || !strings.Contains(err.Error(), "@test: invalid expression") {
		t.Fatalf("error = %v, want @test-specific syntax diagnostic", err)
	}
	pos, ok := compiler.PositionOf(err)
	if !ok || pos != (compiler.Position{File: "fixture.nflow", Line: 1}) {
		t.Fatalf("PositionOf(error) = %+v, %t", pos, ok)
	}
}

func runTestDirective(t *testing.T, lines ...string) (map[string]any, error) {
	t.Helper()
	out := make(map[string]any)
	_, err := Directive.Handler(context.Background(), compiler.DirectiveReq{
		Lines: lines,
		I:     0,
		Out:   out,
		File:  "fixture.nflow",
	})
	return out, err
}
