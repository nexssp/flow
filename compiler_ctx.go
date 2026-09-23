package flow

import (
	"context"
	"slices"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/xctx"
)

// Context keys used by Compile. They live in a separate file so
// compiler.go stays focused on the compilation pipeline; adding a new
// key does not require touching the main file.
//
// xctx.NewKey is used instead of raw context.WithValue for consistency
// with the rest of the flow package and to get typed accessors that
// cannot collide with keys from other packages.
var (
	gatesKey = xctx.NewKey[[]directives.GateRule]("flow.compiler.gates")
)

// WithGates attaches the parsed @gate rules to the compilation context.
// The runner calls this before Execute; a direct caller of Compiler may
// skip it and gates will be empty.
//
// An empty slice is a no-op so callers do not need to guard the call.
func WithGates(ctx context.Context, gates []directives.GateRule) context.Context {
	if len(gates) == 0 {
		return ctx
	}
	return gatesKey.With(ctx, gates)
}

// gatesFromContext reads the rules WithGates stored. Missing key
// returns nil, which applyGates treats as "no @gate rules".
func gatesFromContext(ctx context.Context) []directives.GateRule {
	g, _ := gatesKey.From(ctx)
	return g
}

// applyGates merges @gate rules from the compilation context into the
// graph policy.
//
// "on" rules add the effect class to ApprovalRequiredFor, which the
// rest of Compile already turns into an ApprovalGate check on every
// matching node. "when" rules are recorded on the rule list but have
// no runtime consumer yet; a future evaluator will read them from the
// graph definition. They are deliberately kept in the list instead of
// dropped, so that future consumer does not need a new parser.
//
// Called before Compile(def), because Compile snapshots the definition
// into CompiledGraph.Definition. Modifying def after the fact would
// not be visible to the compiled graph.
func applyGates(ctx context.Context, def *GraphDefinition) {
	gates := gatesFromContext(ctx)
	if len(gates) == 0 {
		return
	}

	for _, g := range gates {
		if g.Kind != "on" {
			continue
		}
		effect := EffectClass(g.Expr)
		if !slices.Contains(def.Policy.ApprovalRequiredFor, effect) {
			def.Policy.ApprovalRequiredFor = append(
				def.Policy.ApprovalRequiredFor, effect)
		}
	}
}
