package core

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xtest/ktest"
)

func TestAnalyze_NilResolverIsInternal(t *testing.T) {
	t.Parallel()
	err := Analyze(nil, &Atom{Name: "x"})
	ktest.RequireErrorKind(t, err, xerr.KindInternal)
}

func TestAnalyze_ContractMismatchIsValidation(t *testing.T) {
	t.Parallel()

	// Field types deliberately differ so the JSON round-trip that
	// coerceCheck relies on cannot succeed: an int cannot be
	// unmarshaled into a string field.
	type in struct{ ID string }
	type out struct{ ID int }

	producer := action.New("validation.producer", func(context.Context, any) (out, error) {
		return out{}, nil
	}).Build()
	consumer := action.New("validation.consumer", func(context.Context, in) (any, error) {
		return nil, nil
	}).Build()

	resolver, err := NewDynamicResolver(action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, consumer},
	})
	ktest.RequireNoError(t, err)

	ast := &PipeExpr{L: &Atom{Name: "validation.producer"}, R: &Atom{Name: "validation.consumer"}}
	err = Analyze(resolver, ast)
	ktest.RequireErrorKind(t, err, xerr.KindValidation)
}

func TestBuild_MissingCapabilityIsNotFound(t *testing.T) {
	t.Parallel()

	resolver, err := NewDynamicResolver(action.Library{Name: "test"})
	ktest.RequireNoError(t, err)

	_, err = Build(context.Background(), resolver, nil, &Atom{Name: "missing"})
	ktest.RequireErrorKind(t, err, xerr.KindNotFound)
}

func TestModifierTable_UnknownModifierIsBadRequest(t *testing.T) {
	t.Parallel()

	table := NewModifierTable()
	target := action.New("x", func(_ context.Context, in any) (any, error) { return in, nil }).Build()

	_, err := table.ApplyAll(target, []string{"unknown"})
	ktest.RequireErrorKind(t, err, xerr.KindBadRequest)
}
