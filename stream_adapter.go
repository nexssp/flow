package flow

import (
	"fmt"

	"github.com/nexssp/kernel/action"
)

// ── Type erasure — what actually happens ─────────────────────────────────────
//
// AnyStream delivers every element as `any`. Apply() below converts that
// to a typed iter.Seq2[In, error], runs the typed operator, and converts
// back. Both type assertions happen PER ELEMENT.
//
// Consequences:
//   - no zero-allocation guarantee per item
//   - any ↔ In/Out conversion cost at each element boundary
//
// Possible future optimization (F1+): when the compiler statically knows
// the full chain (hardcoded pipeline), it can build a direct chain of
// StreamOp[In, Out] without AnyStream in the middle. NOT implemented now —
// requires benchmarking and a decision on priorities.

// typedStreamOperator adapts a typed StreamOp[In, Out] to the erased
// StreamOperator interface.
type typedStreamOperator[In, Out any] struct {
	name string
	op   action.StreamOp[In, Out]
}

// NewTypedStreamOperator wraps a typed operator. Called from
// NamedOperator.Build, where In/Out are statically known (srcpack code).
func NewTypedStreamOperator[In, Out any](
	name string,
	op action.StreamOp[In, Out],
) StreamOperator {
	return &typedStreamOperator[In, Out]{name: name, op: op}
}

func (t *typedStreamOperator[In, Out]) Name() string {
	return t.name
}

// Apply wraps the AnyStream into a typed iterator, runs the operator,
// and re-wraps the output. Cost: two type assertions per element.
func (t *typedStreamOperator[In, Out]) Apply(up action.AnyStream) (action.AnyStream, error) {
	if up == nil {
		return nil, fmt.Errorf("flow: operator %q: nil upstream", t.name)
	}
	typedUp := anyStreamToTyped[In](up, t.name)
	typedOut := t.op(typedUp)
	return typedStreamToAny[Out](typedOut), nil
}

// anyStreamToTyped converts an AnyStream to iter.Seq2[In, error].
// Type assertion per element; a mismatch surfaces as a per-item error.
func anyStreamToTyped[In any](up action.AnyStream, opName string) func(yield func(In, error) bool) {
	return func(yield func(In, error) bool) {
		up(func(item any, err error) bool {
			if err != nil {
				var zero In
				return yield(zero, err)
			}
			typed, ok := item.(In)
			if !ok {
				var zero In
				return yield(zero, fmt.Errorf(
					"flow: operator %q: expected %T, got %T",
					opName, zero, item,
				))
			}
			return yield(typed, nil)
		})
	}
}

// typedStreamToAny converts iter.Seq2[Out, error] back to AnyStream.
// Boxing per element — unavoidable at this boundary.
func typedStreamToAny[Out any](seq func(yield func(Out, error) bool)) action.AnyStream {
	return func(yield func(any, error) bool) {
		seq(func(item Out, err error) bool {
			return yield(any(item), err)
		})
	}
}

// ComposeOperators chains multiple operators into one.
//
// Does NOT validate element types — validation is the compiler's job
// (before Build). This function assumes the operators already match.
//
// The composed operator runs Apply sequentially.
func ComposeOperators(ops ...StreamOperator) (StreamOperator, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("flow: ComposeOperators requires at least one operator")
	}
	if len(ops) == 1 {
		return ops[0], nil
	}

	return &composedOperator{
		ops:  append([]StreamOperator(nil), ops...),
		name: "compose(" + joinArrow(ops) + ")",
	}, nil
}

type composedOperator struct {
	ops  []StreamOperator
	name string
}

func (c *composedOperator) Name() string { return c.name }

func (c *composedOperator) Apply(up action.AnyStream) (action.AnyStream, error) {
	cur := up
	for _, op := range c.ops {
		next, err := op.Apply(cur)
		if err != nil {
			return nil, fmt.Errorf("flow: apply %q: %w", op.Name(), err)
		}
		cur = next
	}
	return cur, nil
}

func joinArrow(ops []StreamOperator) string {
	out := ""
	for i, op := range ops {
		if i > 0 {
			out += " -> "
		}
		out += op.Name()
	}
	return out
}
