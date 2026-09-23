package directives

import "strings"

type atGate struct{}

func init() { Register(atGate{}) }

func (atGate) Name() string { return "gate" }

// Syntax:
//
//	@gate: on high_risk
//	@gate: on side_effect
//	@gate: when cost_usd > 0.10
//	@gate: when attempts >= 3
//
// A "on" rule names an effect class (read_only, side_effect,
// high_risk). The compiler maps it directly onto GraphPolicy.
// ApprovalRequiredFor, which the existing compiler already turns into
// an ApprovalGate check on every matching node.
//
// A "when" rule carries a condition expression over run state. The
// compiler records it, but enforcement requires a downstream consumer
// that evaluates the expression at the matching node.
//
// Multiple rules may be declared; they accumulate.
func (atGate) Apply(ctx *Context, lines []string, i int) (int, error) {
	line := strings.TrimSpace(lines[i])

	rest, ok := StripDirectivePrefix(line, "gate")
	if !ok {
		return 0, AtErr(ctx, i, "gate", "malformed directive")
	}
	if rest == "" {
		return 0, AtErr(ctx, i, "gate", "requires `on EFFECT` or `when EXPR`")
	}

	kind, expr := splitGateKind(rest)
	if kind == "" {
		return 0, AtErrf(ctx, i, "gate",
			"expected `on EFFECT` or `when EXPR`, got %q", rest)
	}
	if expr == "" {
		return 0, AtErrf(ctx, i, "gate "+kind,
			"missing value after %q", kind)
	}

	if kind == "on" {
		switch strings.ToLower(expr) {
		case "read_only", "side_effect", "high_risk":
		default:
			return 0, AtErrf(ctx, i, "gate on",
				"unknown effect %q (known: read_only, side_effect, high_risk)", expr)
		}
	}

	ctx.Out.Gates = append(ctx.Out.Gates, GateRule{
		Kind: kind,
		Expr: expr,
		Pos:  Position{File: ctx.File, Line: i + 1},
	})

	return i + 1, nil
}

// splitGateKind returns ("on", "high_risk") for "on high_risk",
// ("when", "cost_usd > 0.10") for "when cost_usd > 0.10", and
// ("", "") when neither keyword is present.
func splitGateKind(s string) (kind, expr string) {
	s = strings.TrimSpace(s)

	for _, k := range []string{"on", "when"} {
		if !strings.HasPrefix(s, k) {
			continue
		}
		after := s[len(k):]
		if after == "" {
			return k, ""
		}
		if after[0] != ' ' && after[0] != '\t' {
			continue
		}
		return k, strings.TrimSpace(after)
	}
	return "", ""
}
