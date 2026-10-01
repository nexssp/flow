package core

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xtest/ktest"
)

// Same field name, incompatible types. coerceCheck marshals the
// producer and unmarshals into the consumer; a string cannot land in
// an int field, so the mismatch is detected. Differently-named fields
// would silently succeed via encoding/json's ignore-unknown-key
// behavior and these tests would pass vacuously.
type (
	analyzeIn  struct{ ID int }
	analyzeOut struct{ ID string }
)

func mustResolver(tb testing.TB, libs ...action.Library) *DynamicResolver {
	tb.Helper()
	resolver, err := NewDynamicResolver(libs...)
	ktest.RequireNoError(tb, err)
	return resolver
}

func typedAction[Req, Res any](name string) *action.BuiltAction[Req, Res] {
	return action.New(name, func(context.Context, Req) (Res, error) {
		var zero Res
		return zero, nil
	}).Build()
}

// testPipe and testParallel are the minimal operator table the analyzer
// test needs to build PipeExpr and ParallelExpr from source text. Kept
// inline because importing extensions/syntax would create an import
// cycle: extensions/syntax imports core.
func testPipe() Operator {
	return Operator{
		Meta: OperatorMeta{
			Name:          "pipe",
			Token:         TokArrow,
			Precedence:    7,
			Associativity: Left,
		},
		Handler: func(_ context.Context, req OperatorReq) (OperatorRes, error) {
			return OperatorRes{Node: &PipeExpr{L: req.Left, R: req.Right}}, nil
		},
	}
}

func testParallel() Operator {
	return Operator{
		Meta: OperatorMeta{
			Name:          "parallel",
			Token:         TokAmpersand,
			Precedence:    5,
			Associativity: Left,
		},
		Handler: func(_ context.Context, req OperatorReq) (OperatorRes, error) {
			return OperatorRes{
				Node: &ParallelExpr{Branches: []Expr{req.Left, req.Right}},
			}, nil
		},
	}
}

func testTable() *OperatorTable {
	return NewOperatorTable(testPipe(), testParallel())
}

// ── Tests ────────────────────────────────────────────────────────────

func TestAnalyze_NoResolver(t *testing.T) {
	t.Parallel()
	err := Analyze(nil, &Atom{Name: "any"})
	ktest.RequireErrorContains(t, err, "capability resolver is nil")
}

func TestAnalyze_SingleAtomAlwaysPasses(t *testing.T) {
	t.Parallel()
	err := Analyze(mustResolver(t), &Atom{Name: "not-registered"})
	ktest.RequireNoError(t, err)
}

func TestAnalyze_PipeEdges(t *testing.T) {
	t.Parallel()

	producer := typedAction[any, analyzeOut]("producer")
	consumerGood := typedAction[analyzeOut, any]("consumer_good")
	consumerBad := typedAction[analyzeIn, any]("consumer_bad")
	resolver := mustResolver(t, action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, consumerGood, consumerBad},
	})

	cases := []struct {
		name    string
		left    string
		right   string
		wantErr bool
		errHint string
	}{
		{"compatible types", "producer", "consumer_good", false, ""},
		{"incompatible types", "producer", "consumer_bad", true, "contract mismatch"},
		{"unknown left skipped", "missing", "consumer_good", false, ""},
		{"unknown right skipped", "producer", "missing", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ast := &PipeExpr{
				L: &Atom{Name: tc.left},
				R: &Atom{Name: tc.right},
			}
			err := Analyze(resolver, ast)
			if tc.wantErr {
				ktest.RequireErrorContains(t, err, tc.errHint)
				return
			}
			ktest.RequireNoError(t, err)
		})
	}
}

func TestAnalyze_ParallelChecksEachChild(t *testing.T) {
	t.Parallel()

	producer := typedAction[any, analyzeOut]("producer")
	bad := typedAction[analyzeIn, any]("bad")
	resolver := mustResolver(t, action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, bad},
	})

	ast := &ParallelExpr{
		Branches: []Expr{
			&PipeExpr{L: &Atom{Name: "producer"}, R: &Atom{Name: "bad"}},
			&Atom{Name: "producer"},
		},
	}

	err := Analyze(resolver, ast)
	ktest.RequireErrorContains(t, err, "contract mismatch")
}

func TestAnalyze_FallbackChecksBothArms(t *testing.T) {
	t.Parallel()

	producer := typedAction[any, analyzeOut]("producer")
	bad := typedAction[analyzeIn, any]("bad")
	resolver := mustResolver(t, action.Library{
		Name:    "test",
		Actions: []action.AnyAction{producer, bad},
	})

	ast := &FallbackExpr{
		L: &Atom{Name: "producer"},
		R: &PipeExpr{L: &Atom{Name: "producer"}, R: &Atom{Name: "bad"}},
	}

	err := Analyze(resolver, ast)
	ktest.RequireErrorContains(t, err, "contract mismatch")
}
