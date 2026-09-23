package directives

import (
	"strings"
	"testing"
)

func TestAtGate_OnHighRisk(t *testing.T) {
	ctx := newTestCtx()
	next, err := atGate{}.Apply(ctx, []string{`@gate: on high_risk`}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}
	if len(ctx.Out.Gates) != 1 {
		t.Fatalf("gates = %v", ctx.Out.Gates)
	}
	g := ctx.Out.Gates[0]
	if g.Kind != "on" || g.Expr != "high_risk" {
		t.Errorf("rule = %+v", g)
	}
	if g.Pos.Line != 1 {
		t.Errorf("pos line = %d, want 1", g.Pos.Line)
	}
}

func TestAtGate_WhenWithExpression(t *testing.T) {
	ctx := newTestCtx()
	_, err := atGate{}.Apply(ctx, []string{`@gate: when cost_usd > 0.10`}, 0)
	if err != nil {
		t.Fatal(err)
	}
	g := ctx.Out.Gates[0]
	if g.Kind != "when" || g.Expr != "cost_usd > 0.10" {
		t.Errorf("rule = %+v", g)
	}
}

func TestAtGate_RejectsUnknownEffect(t *testing.T) {
	ctx := newTestCtx()
	_, err := atGate{}.Apply(ctx, []string{`@gate: on catastrophic`}, 0)
	if err == nil || !strings.Contains(err.Error(), "unknown effect") {
		t.Fatalf("expected unknown effect error, got %v", err)
	}
}

func TestAtGate_RejectsMissingKeyword(t *testing.T) {
	ctx := newTestCtx()
	_, err := atGate{}.Apply(ctx, []string{`@gate: high_risk`}, 0)
	if err == nil || !strings.Contains(err.Error(), "expected `on") {
		t.Fatalf("expected keyword error, got %v", err)
	}
}

func TestAtGate_RejectsMissingValue(t *testing.T) {
	ctx := newTestCtx()
	_, err := atGate{}.Apply(ctx, []string{`@gate: on`}, 0)
	if err == nil || !strings.Contains(err.Error(), "missing value") {
		t.Fatalf("expected missing value, got %v", err)
	}
}

func TestAtGate_Accumulates(t *testing.T) {
	ctx := newTestCtx()
	d := atGate{}

	_, _ = d.Apply(ctx, []string{`@gate: on high_risk`}, 0)
	_, _ = d.Apply(ctx, []string{`@gate: when cost_usd > 0.10`}, 0)
	_, _ = d.Apply(ctx, []string{`@gate: on side_effect`}, 0)

	if len(ctx.Out.Gates) != 3 {
		t.Fatalf("expected 3 gates, got %d", len(ctx.Out.Gates))
	}
	if ctx.Out.Gates[0].Expr != "high_risk" {
		t.Errorf("gates[0] = %+v", ctx.Out.Gates[0])
	}
	if ctx.Out.Gates[2].Expr != "side_effect" {
		t.Errorf("gates[2] = %+v", ctx.Out.Gates[2])
	}
}

func TestAtGate_AllThreeEffectClasses(t *testing.T) {
	for _, eff := range []string{"read_only", "side_effect", "high_risk"} {
		t.Run(eff, func(t *testing.T) {
			ctx := newTestCtx()
			_, err := atGate{}.Apply(ctx, []string{`@gate: on ` + eff}, 0)
			if err != nil {
				t.Fatalf("effect %q rejected: %v", eff, err)
			}
		})
	}
}
