package compiler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/file"

	"github.com/nexssp/flow/compiler"
)

func TestCompileExpression_BareBuiltinHint(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"count", "type", "len", "min", "max"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := compiler.CompileExpression(name+" > 4", expr.AllowUndefinedVariables())
			if err == nil || !strings.Contains(err.Error(), "for a payload field use ."+name) {
				t.Fatalf("compile bare %s = %v, want targeted field hint", name, err)
			}
			sourceErr, ok := errors.AsType[*file.Error](err)
			if !ok || sourceErr == nil {
				t.Fatalf("original expr error was not preserved: %v", err)
			}
		})
	}
}

func TestCompileExpression_NoFalseBuiltinHints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		options []expr.Option
		valid   bool
	}{
		{"calls", `len([1, 2]) == 2 && count([1, 2], # > 0) == 2`, nil, true},
		{"literal text", `"count" > 4`, nil, false},
		{"member", `user.count > false`, []expr.Option{expr.Env(map[string]any{"user": map[string]any{"count": 5}})}, false},
		{"local shadow disallowed by expr", `let count = 5; count > 4`, nil, false},
		{"local shadow with builtin disabled", `let count = 5; count > 4`, []expr.Option{expr.DisableBuiltin("count")}, true},
		{"local invalid", `let count = "five"; count > 4`, nil, false},
		{"env shadow", `count > 4`, []expr.Option{expr.Env(map[string]any{"count": 5})}, true},
		{"invalid env shadow", `count > 4`, []expr.Option{expr.Env(map[string]any{"count": "five"})}, false},
		{"disabled", `count > 4`, []expr.Option{expr.DisableBuiltin("count"), expr.AllowUndefinedVariables()}, true},
		{"custom function", `count > 4`, []expr.Option{expr.Function("count", func(...any) (any, error) { return 5, nil })}, false},
		{"dynamic field", compiler.PreprocessDots(`.count > 4`), []expr.Option{expr.AllowUndefinedVariables()}, true},
		{"valid function value", `count`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := compiler.CompileExpression(tc.source, tc.options...)
			if (err == nil) != tc.valid {
				t.Fatalf("compile = %v, want valid %v", err, tc.valid)
			}
			if err != nil && strings.Contains(err.Error(), "for a payload field use") {
				t.Fatalf("false builtin hint: %v", err)
			}
		})
	}
}

func TestCompileExpressionNumericSeparators(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		source string
		valid  bool
	}{
		{name: "integer", source: `1_123_123 == 1123123`, valid: true},
		{name: "fraction", source: `1_234.5_6 == 1234.56`, valid: true},
		{name: "hexadecimal", source: `0xCA_FE == 0xCAFE`, valid: true},
		{name: "base prefix separator", source: `0x_FF == 255`, valid: true},
		{name: "binary", source: `0b10_01 == 9`, valid: true},
		{name: "identifier and string underscores", source: `amount_minor == "item_123"`, valid: true},
		{name: "repeated separator", source: `1__123 == 1123`},
		{name: "trailing separator", source: `1_ == 1`},
		{name: "separator beside decimal point", source: `1._23 == 1.23`},
		{name: "repeated base-prefix separator", source: `0x__FF == 255`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := compiler.CompileExpression(test.source, expr.AllowUndefinedVariables())
			if (err == nil) != test.valid {
				t.Fatalf("CompileExpression(%q) error = %v, want valid %t", test.source, err, test.valid)
			}
			if !test.valid && !strings.Contains(err.Error(), "invalid numeric separator") {
				t.Fatalf("error = %v, want invalid numeric separator", err)
			}
		})
	}
}
