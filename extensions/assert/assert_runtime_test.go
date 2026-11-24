package assert_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/nexssp/kernel/xerr"

	assertext "github.com/nexssp/flow/extensions/assert"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
)

func TestRuntimeMessageExpressionFixture(t *testing.T) {
	t.Parallel()
	fixture, err := fs.ReadFile(assertext.Bundle(nil).Fixtures, "nflows/message_expression.nflow")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}

	if _, err := runner.Execute(t.Context(), cfg, string(fixture), "message_expression.nflow", map[string]any{
		"expected": 100,
		"actual":   100,
	}); err != nil {
		t.Fatalf("execute passing assertion: %v", err)
	}

	_, err = runner.Execute(t.Context(), cfg, string(fixture), "message_expression.nflow", map[string]any{
		"expected": 100,
		"actual":   92,
	})
	if err == nil || xerr.KindFrom(err) != xerr.KindValidation {
		t.Fatalf("error = %v, want a validation error", err)
	}
	if !strings.Contains(err.Error(), "expected 100, got 92") {
		t.Errorf("error %q does not contain the expected-versus-actual detail", err)
	}
}

func TestRuntimeMessageExpressionValidationAndFallback(t *testing.T) {
	t.Parallel()
	cfg, err := runner.BuildConfig(native.SelftestBundles())
	if err != nil {
		t.Fatalf("build self-test config: %v", err)
	}

	_, err = runner.Execute(t.Context(), cfg, `assert(.ok, .count)`, "message_lazy.nflow", map[string]any{
		"ok": true, "count": 42,
	})
	if err != nil {
		t.Fatalf("passing assertion evaluated its non-string message: %v", err)
	}

	cases := []struct {
		name   string
		source string
		input  map[string]any
		want   string
	}{
		{
			name:   "non-string message",
			source: `assert(.ok, .count)`,
			input:  map[string]any{"ok": false, "count": 42},
			want:   "returned int, want string",
		},
		{
			name:   "message evaluation error",
			source: `assert(.ok, .count.name)`,
			input:  map[string]any{"ok": false, "count": 42},
			want:   "could not be evaluated",
		},
		{
			name:   "default condition detail",
			source: `assert(.count > 0)`,
			input:  map[string]any{"count": -1},
			want:   `condition ".count > 0" evaluated false`,
		},
		{
			name:   "quoted expression operands are not a static message",
			source: `assert(false, "a" + "b")`,
			input:  nil,
			want:   "ab",
		},
		{
			name:   "empty message expression falls back to condition detail",
			source: `assert(false, .empty)`,
			input:  map[string]any{"empty": ""},
			want:   `assert: condition "false" evaluated false`,
		},
		{
			name:   "non-boolean condition",
			source: `assert(.condition)`,
			input:  map[string]any{"condition": 42},
			want:   `condition ".condition" returned int, want bool`,
		},
		{
			name:   "condition evaluation error",
			source: `assert(.missing.field)`,
			input:  map[string]any{"missing": nil},
			want:   `condition ".missing.field" could not be evaluated`,
		},
		{
			name:   "static message remains unchanged",
			source: `assert(false, "static failure")`,
			input:  nil,
			want:   "static failure",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, runErr := runner.Execute(t.Context(), cfg, testCase.source, testCase.name+".nflow", testCase.input)
			if runErr == nil || xerr.KindFrom(runErr) != xerr.KindValidation || !strings.Contains(runErr.Error(), testCase.want) {
				t.Fatalf("error = %v, want validation error containing %q", runErr, testCase.want)
			}
		})
	}
}
