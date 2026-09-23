package nodes

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/expr-lang/expr"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

var projectionCounter atomic.Int64

// NewProjectionAction compiles a projection body into a Kernel action.
//
// The body is the inner content of a `{ ... }` projection in a flow
// pipeline, e.g. `attempt: attempt + 1, feedback: ...`. The parser
// (compiler/parseProjection) strips the outer braces before handing
// the body to this function.
//
// # Plain projection
//
// A body without `...` produces a fresh object containing exactly the
// fields declared in the body:
//
//	{ to: .name, subject: "Welcome " + .tier }
//
// # Spread projection
//
// A body that begins with `...` starts from the previous step's
// output and overrides only the fields explicitly listed:
//
//	{ ... }                          →  pass-through of the input
//	{ ..., attempt: attempt + 1 }    →  keep all fields, bump attempt
//
// The spread token must be the first entry. Its output is exactly the
// input map with the listed fields overridden; no reserved names are
// added to the result.
//
// # Reserved identifiers
//
// Inside the body the following identifiers are available in addition
// to the input's own fields:
//
//	__root__   — the raw, un-normalised input value
//	__state__  — the normalised object form of the input
//
// `__state__` is what `...` expands to. Both are namespaced with `__`
// to avoid clashing with ordinary user data.
//
// # Array functions and the pipe operator
//
// The projection body is evaluated by expr-lang and therefore has
// access to every built-in array function and to the pipe operator.
// The DSL layer does not restrict this surface; it only ensures that
// the expressions survive the two preprocessors (dot-notation rewrite
// and spread rewrite) unchanged.
//
// Commonly used functions and shapes:
//
//	sortBy(array, #.field [, "asc"|"desc"])
//	groupBy(array, #.field)
//	filter(array, #.field == value)
//	map(array, #.field)
//	reduce(array, #acc + #.field, initial)
//	count(array [, #.field == value])
//	uniq(array)
//	flatten(array)
//	concat(a, b, ...)
//	first(array)
//	last(array)
//	take(array, n)
//	reverse(array)
//	toJSON(value)
//	fromJSON(string)
//	len(collection)
//
// The predicate scope marker is `#`, which refers to the current
// element. The dot-notation preprocessor preserves `#.field` exactly
// as written; a bare `.field` outside a predicate is rewritten to
// `field` to match the flattened environment produced by
// NormalizeEnv.
//
// A realistic chained projection:
//
//	{
//	  ...,
//	  critical: filter(findings, #.severity == "critical"),
//	  top:      findings | sortBy(#.line, "desc") | take(10),
//	  count:    count(findings, #.severity == "warning"),
//	}
//
// # Nil safety
//
// Two operators from expr-lang are especially useful for optional
// fields:
//
//	feedback ?? "none"       →  fallback when feedback is nil
//	patch?.source_code       →  safe field access on a possibly nil object
func NewProjectionAction(body string) (*action.BuiltAction[any, any], error) {
	code, usesSpread, err := PreprocessSpread(body)
	if err != nil {
		return nil, err
	}

	compiled, usesRoot := PreprocessDotNotation(code)

	program, err := expr.Compile(
		compiled,
		expr.AllowUndefinedVariables(),
		spreadMergeOpt,
	)
	if err != nil {
		return nil, xerr.BadRequest(fmt.Sprintf(
			"flow: invalid projection %q: %v", body, err,
		))
	}

	nodeID := fmt.Sprintf("projection_%d", projectionCounter.Add(1))

	return action.New(nodeID, func(_ context.Context, input any) (any, error) {
		env := buildProjectionEnv(input, usesRoot, usesSpread)

		out, runErr := expr.Run(program, env)
		if runErr != nil {
			return nil, xerr.Internal(
				fmt.Sprintf("flow: projection %q failed", body), runErr,
			)
		}

		return out, nil
	}).
		Internal().
		Build(), nil
}
