package schema

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/xtest/ktest"

	compiler "github.com/nexssp/flow/compiler"
)

func TestDirective_TypedSymbolsResolveDefaultsAndValidationByFieldType(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"limits": {Name: "limits", Symbols: map[string]any{
				"status":  compiler.TypedSymbolValue{Type: "int", Value: 201},
				"timeout": compiler.TypedSymbolValue{Type: "duration", Value: 5 * time.Second},
				"label":   compiler.TypedSymbolValue{Type: "string", Value: "created"},
			}},
		},
	}
	lines := []string{
		`@schema Request {`,
		`  Status int ` + "`json:\"status\" default:\"limits.status\" validate:\"eq=limits.status\"`",
		`  Timeout duration ` + "`json:\"timeout\" default:\"limits.timeout\" validate:\"gte=limits.timeout\"`",
		`  Label string ` + "`json:\"label\" default:\"limits.label\" validate:\"oneof=limits.label\"`",
		`}`,
	}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: lines, I: 0, Out: out, File: "typed-request.nflow",
	})
	ktest.RequireNoError(t, err)
	schema := SchemasFromMap(out)[0]
	values, err := ApplyDefaultsAndValidate(context.Background(), schema, map[string]any{})
	ktest.RequireNoError(t, err)
	ktest.RequireEqual[any](t, values["status"], 201)
	ktest.RequireEqual[any](t, values["timeout"], 5*time.Second)
	ktest.RequireEqual[any](t, values["label"], "created")
	ktest.RequireErrorContains(t, ValidateWithoutDefaults(context.Background(), schema, map[string]any{
		"status": 202, "timeout": 5 * time.Second, "label": "created",
	}), `failed`)
}

func TestDirective_RejectsTypedDefaultMismatchAtFieldLocation(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"values": {Name: "values", Symbols: map[string]any{
				"status": compiler.TypedSymbolValue{Type: "string", Value: "201"},
			}},
		},
	}
	lines := []string{
		`@schema Request {`,
		`  Status int ` + "`json:\"status\" default:\"values.status\"`",
		`}`,
	}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: lines, I: 0, Out: out, File: "typed-request.nflow",
	})
	ktest.RequireErrorContains(t, err, "incompatible with default field type")
	position, ok := compiler.PositionOf(err)
	ktest.RequireCondition(t, ok, "typed mismatch must retain a source position")
	ktest.RequireEqual(t, position.File, "typed-request.nflow")
	ktest.RequireEqual(t, position.Line, 2)
}

func TestDirective_RejectsTypedSymbolForStringBasedUnsupportedRule(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		"symbols": map[string]compiler.SymbolNamespace{
			"values": {Name: "values", Symbols: map[string]any{
				"status": compiler.TypedSymbolValue{Type: "int", Value: 201},
			}},
		},
	}
	lines := []string{
		`@schema Request {`,
		`  Name string ` + "`json:\"name\" validate:\"email=values.status\"`",
		`}`,
	}
	_, err := handleDirective(context.Background(), compiler.DirectiveReq{
		Lines: lines, I: 0, Out: out, File: "typed-request.nflow",
	})
	ktest.RequireErrorContains(t, err, "incompatible with validation rule")
	position, ok := compiler.PositionOf(err)
	ktest.RequireCondition(t, ok, "typed rule mismatch must retain a source position")
	ktest.RequireEqual(t, position.Line, 2)
}

func TestDirective_TypedDurationValidationUsesNanoseconds(t *testing.T) {
	t.Parallel()
	field := Field{
		Name: "Timeout", JSONName: "timeout", Type: "duration", Kind: KindDuration,
		Tags:     map[string]string{"default": "limits.timeout", "validate": "gte=limits.timeout"},
		Position: compiler.Position{File: "typed-duration.nflow", Line: 4},
	}
	local := map[string]compiler.SymbolNamespace{
		"limits": {Name: "limits", Symbols: map[string]any{
			"timeout": compiler.TypedSymbolValue{Type: "duration", Value: 5 * time.Second},
		}},
	}
	fields := []Field{field}
	ktest.RequireNoError(t, resolveSchemaSymbols(fields, local))
	ktest.RequireNoError(t, validateSchemaDefaults(fields))
	_, err := ApplyDefaultsAndValidate(context.Background(), Schema{Name: "Response", Fields: fields}, map[string]any{
		"timeout": 6 * time.Second,
	})
	ktest.RequireNoError(t, err)
	_, err = ApplyDefaultsAndValidate(context.Background(), Schema{Name: "Response", Fields: fields}, map[string]any{
		"timeout": 4 * time.Second,
	})
	ktest.RequireErrorContains(t, err, "at least 5000000000")
}
