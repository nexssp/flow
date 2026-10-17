package compiler

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/xtest/ktest"
)

func TestParseModifierBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		suffix string
		want   []string
	}{
		{
			name:   "reported value followed by flags",
			suffix: ` :rate_limit=50 :idempotent :dedup :coalesce`,
			want:   []string{"rate_limit=50", "idempotent", "dedup", "coalesce"},
		},
		{
			name:   "compact value followed by flags",
			suffix: `:rate_limit=50:idempotent:dedup:coalesce`,
			want:   []string{"rate_limit=50", "idempotent", "dedup", "coalesce"},
		},
		{
			name:   "interleaved flags and values",
			suffix: ` :idempotent :timeout=2s :dedup :retry=3 :coalesce :rate_limit=50`,
			want:   []string{"idempotent", "timeout=2s", "dedup", "retry=3", "coalesce", "rate_limit=50"},
		},
		{
			name:   "quoted punctuation is data",
			suffix: ` :desc="Maks: ready, Julia: retry=3 ? yes : no" :dedup :tag="resilience,core" :coalesce`,
			want:   []string{"desc=Maks: ready, Julia: retry=3 ? yes : no", "dedup", "tag=resilience,core", "coalesce"},
		},
		{
			name:   "quoted URL and route",
			suffix: ` :route="POST /jobs/:id" :desc="https://example.test:8443/jobs" :idempotent`,
			want:   []string{"route=POST /jobs/:id", "desc=https://example.test:8443/jobs", "idempotent"},
		},
		{
			name:   "single and backtick quoted values",
			suffix: " :desc='Maks: ready' :dedup :tag=`Julia: core` :coalesce",
			want:   []string{"desc=Maks: ready", "dedup", "tag=Julia: core", "coalesce"},
		},
		{
			name:   "multiline flags after value",
			suffix: " :rate_limit=50\n :idempotent\n :dedup\n :coalesce",
			want:   []string{"rate_limit=50", "idempotent", "dedup", "coalesce"},
		},
		{
			name:   "existing comma value",
			suffix: ` :tag=resilience,core :dedup`,
			want:   []string{"tag=resilience,core", "dedup"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, primary := range []string{"noop", `{ value: "Maks" }`} {
				t.Run(primary, func(t *testing.T) {
					p := NewParserWithPrimaries(context.Background(), NewOperatorTable(),
						NewPrimaryExtensionTable(testProjectionPrimary{}), primary+tc.suffix)
					expr, err := p.Parse()
					ktest.RequireNoError(t, err)
					switch node := expr.(type) {
					case *Atom:
						ktest.RequireEqual(t, node.Modifiers, tc.want)
					case *DecoratedExpr:
						ktest.RequireEqual(t, node.Modifiers, tc.want)
					default:
						t.Fatalf("parsed %T, want atom or decorated projection", expr)
					}
				})
			}
		})
	}
}

func TestParseModifierBoundaryPreservesTernary(t *testing.T) {
	t.Parallel()
	for _, primary := range []string{"noop", `{ value: "Maks" }`} {
		t.Run(primary, func(t *testing.T) {
			p := NewParserWithPrimaries(context.Background(), NewOperatorTable(),
				NewPrimaryExtensionTable(testProjectionPrimary{}),
				`true ? `+primary+` :rate_limit=50 :idempotent :dedup :coalesce : fallback :timeout=2s`)
			expr, err := p.Parse()
			ktest.RequireNoError(t, err)
			conditional, ok := expr.(*ConditionalExpr)
			ktest.RequireCondition(t, ok, "got %T, want conditional", expr)
			fallback, ok := conditional.Else.(*Atom)
			ktest.RequireCondition(t, ok, "else = %T, want atom", conditional.Else)
			ktest.RequireEqual(t, fallback.Name, "fallback")
			ktest.RequireEqual(t, fallback.Modifiers, []string{"timeout=2s"})
			want := []string{"rate_limit=50", "idempotent", "dedup", "coalesce"}
			switch then := conditional.Then.(type) {
			case *Atom:
				ktest.RequireEqual(t, then.Modifiers, want)
			case *DecoratedExpr:
				ktest.RequireEqual(t, then.Modifiers, want)
			default:
				t.Fatalf("then = %T, want atom or decorated projection", conditional.Then)
			}
		})
	}
}

func TestParseModifierBoundaryRejectsMissingValue(t *testing.T) {
	t.Parallel()
	for _, src := range []string{`noop :rate_limit= :dedup`, `noop :rate_limit=`, `{ value: "Julia" } :timeout= :coalesce`} {
		p := NewParserWithPrimaries(context.Background(), NewOperatorTable(),
			NewPrimaryExtensionTable(testProjectionPrimary{}), src)
		_, err := p.Parse()
		ktest.RequireCondition(t, err != nil, "%q should reject a missing modifier value", src)
	}
}
