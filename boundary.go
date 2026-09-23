package flow

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// BoundarySpec describes a stream-to-unary adapter. Boundary atoms are
// the only places where a flow pipeline switches from stream mode to
// unary mode.
//
// A pipeline is a left-to-right sequence of nodes connected by `->`.
// When a boundary atom appears, the compiler:
//
//  1. compiles everything to its left as a stream source,
//  2. asks the boundary to turn that stream source into a unary action,
//  3. compiles everything to its right as an ordinary unary chain, and
//  4. pipes the three pieces together with a plain sequential pipe.
//
// Boundary atoms never appear in the flow registry, never carry
// modifiers, and never appear in the action catalog. They are pure
// compiler directives that happen to share the atom surface syntax.
//
// # Precedence
//
// If an atom name is both registered in the flow registry and present
// in the boundary registry, the boundary wins. This is intentional:
// boundary names are reserved by convention, and using a boundary name
// for a normal action is a mistake.
type BoundarySpec struct {
	// Name is the atom name recognised in a pipeline, e.g. "collect".
	Name string

	// Description is a short human-readable summary used by diagnostics
	// and documentation.
	Description string

	// Consume wraps a stream source into a unary action. The returned
	// builder produces exactly one value per invocation; that value is
	// forwarded to the next node in the pipeline as if it were the
	// request of an ordinary unary action.
	//
	// Consume must not mutate the upstream source. It may wrap the
	// source in an adapter that drains the stream once per request.
	Consume func(upstream action.AnyStreamAction) *action.Builder[any, any]
}

// boundaryRegistry holds the boundary specs registered at package init
// time. It is intentionally not a flow.Registry because boundaries are
// a compile-time concept, not a runtime one.
var boundaryRegistry = map[string]BoundarySpec{}

// RegisterBoundary registers a boundary spec. It panics on duplicate
// names or on a nil Consume, because these are programmer errors that
// should surface during init rather than at compile time.
//
// Registration is intended to happen in package init functions and in
// tests. It is not safe for concurrent use; callers must ensure that
// registration completes before any pipeline is compiled.
func RegisterBoundary(spec BoundarySpec) {
	if spec.Name == "" {
		panic("flow: RegisterBoundary called with an empty name")
	}
	if spec.Consume == nil {
		panic("flow: RegisterBoundary(" + spec.Name + ") called with a nil Consume")
	}
	if _, dup := boundaryRegistry[spec.Name]; dup {
		panic("flow: duplicate boundary registration for " + spec.Name)
	}
	boundaryRegistry[spec.Name] = spec
}

// BoundaryByName looks up a boundary by its atom name. The second
// return value reports whether a boundary with that name exists.
func BoundaryByName(name string) (BoundarySpec, bool) {
	spec, ok := boundaryRegistry[name]
	return spec, ok
}

// NamedBoundaries returns every registered boundary sorted by name.
// The result is a copy; the caller may modify it freely.
func NamedBoundaries() []BoundarySpec {
	out := make([]BoundarySpec, 0, len(boundaryRegistry))
	for _, spec := range boundaryRegistry {
		out = append(out, spec)
	}
	slices.SortFunc(out, func(a, b BoundarySpec) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Built-in boundaries
// ─────────────────────────────────────────────────────────────────────────────

func init() {
	RegisterBoundary(BoundarySpec{
		Name:        "collect",
		Description: "Drains the upstream stream into a []any slice",
		Consume:     collectConsume,
	})
}

// collectConsume materialises a stream into a []any.
//
// The upstream meta name is preserved on the returned action so that
// diagnostics point at the source head rather than at the boundary
// itself. That is why a pipeline like:
//
//	fs.walk -> fs.read -> collect
//
// produces an action named "fs.walk" in summaries, not "collect".
func collectConsume(upstream action.AnyStreamAction) *action.Builder[any, any] {
	name := upstream.Describe().Name

	return action.New(name, func(ctx context.Context, req any) (any, error) {
		stream, err := upstream.DoStreamAny(ctx, req)
		if err != nil {
			return nil, err
		}
		if stream == nil {
			return []any{}, nil
		}

		out := make([]any, 0, 64)

		var firstErr error

		stream(func(item any, itemErr error) bool {
			if itemErr != nil {
				firstErr = itemErr
				return false
			}
			out = append(out, item)
			return true
		})

		if firstErr != nil {
			return out, xerr.Internal(
				fmt.Sprintf("flow: collect from %q failed", name), firstErr,
			)
		}
		return out, nil
	}).
		Internal().
		Description("Stream boundary: materialises the upstream stream into a slice").
		Tag("flow", "boundary")
}
