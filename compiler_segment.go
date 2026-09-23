package flow

import (
	"fmt"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

// compileSegmentedPipeline compiles a pipeline that contains at least
// one boundary atom.
//
// The pipeline is flattened into a linear sequence of top-level nodes
// (atoms, projections, parallels, conditionals, loops — anything that
// sits between two `->`). The sequence is then split at boundary atoms.
// Each resulting segment is compiled in the mode that its leading node
// implies:
//
//   - the segment before the boundary runs as a stream pipeline and is
//     terminated by the boundary's Consume function;
//   - the segment after the boundary runs as an ordinary unary chain.
//
// The three pieces are chained with a plain sequential pipe, so the
// boundary behaves as if it were a unary adapter between the two modes.
//
// # Scope
//
// Only one boundary per pipeline is supported. Supporting multiple
// boundaries would require a general "stream → unary → stream" bridge,
// which is not needed by any current use case. The restriction is
// enforced with a clear error message so that an unsupported pipeline
// fails at compile time rather than at run time.
//
// # Position rules
//
//   - The boundary must not be the first node: there is nothing to
//     consume.
//   - Everything before the boundary must form a valid stream source
//     chain; otherwise the underlying compiler reports the error.
//   - The boundary may be the last node: the pipeline then produces
//     the slice directly.
func compileSegmentedPipeline(
	ast compiler.Expr,
	reg *action.Registry,
	options *compileOptions,
) (*action.Builder[any, any], error) {
	nodes := flattenLinear(ast)

	segments, boundaries, err := splitAtBoundaries(nodes)
	if err != nil {
		return nil, err
	}
	if len(boundaries) == 0 {
		return nil, xerr.Internal("flow: segmented compilation selected without a boundary")
	}
	if len(boundaries) > 1 {
		names := make([]string, 0, len(boundaries))
		for _, b := range boundaries {
			names = append(names, b.Name)
		}
		return nil, xerr.BadRequest(fmt.Sprintf(
			"flow: only one stream boundary per pipeline is supported, found %d (%v)",
			len(boundaries), names,
		))
	}
	if len(segments) < 2 || len(segments[0]) == 0 {
		return nil, xerr.BadRequest(fmt.Sprintf(
			"flow: %q cannot appear at the start of a pipeline", boundaries[0].Name,
		))
	}

	streamSrc, err := buildStreamSource(pipelineFromSlice(segments[0]), options)
	if err != nil {
		return nil, err
	}

	current := boundaries[0].Consume(streamSrc).Build()

	if len(segments) > 1 && len(segments[1]) > 0 {
		post, err := compileLinearUnary(segments[1], reg, options)
		if err != nil {
			return nil, err
		}

		current = action.Pipe[any, any, any](
			"flow.boundary.chain",
			current,
			post.Build(),
		).Build()
	}

	return action.Dynamic(current), nil
}

// flattenLinear walks a left-associative PipelineExpr and returns the
// sequence of top-level nodes from left to right.
//
//	A -> B -> C   (represented as Pipeline(Pipeline(A, B), C))
//	becomes       [A, B, C]
//
// Non-pipeline expressions are returned as a single-element slice. The
// function does not recurse into ParallelExpr, ConditionalExpr, or
// LoopExpr: boundary detection only cares about the top-level `->`
// structure, and nested constructs are opaque to it.
func flattenLinear(expr compiler.Expr) []compiler.Expr {
	switch n := expr.(type) {
	case *compiler.PipelineExpr:
		return append(flattenLinear(n.Left), n.Right)
	default:
		return []compiler.Expr{expr}
	}
}

// pipelineFromSlice rebuilds a left-associative PipelineExpr chain
// from a linear slice. It is the inverse of flattenLinear for chains
// whose elements are already in source order.
func pipelineFromSlice(nodes []compiler.Expr) compiler.Expr {
	if len(nodes) == 0 {
		return nil
	}
	expr := nodes[0]
	for _, n := range nodes[1:] {
		expr = &compiler.PipelineExpr{Left: expr, Right: n}
	}
	return expr
}

// atomName returns the atom name if expr is an AtomExpr, otherwise "".
// It is the only place that inspects the concrete AST node type during
// boundary detection.
func atomName(expr compiler.Expr) string {
	if a, ok := expr.(*compiler.AtomExpr); ok {
		return a.Name
	}
	return ""
}

// splitAtBoundaries partitions nodes into segments separated by
// boundary atoms.
//
// Given nodes = [s0, s1, B, s2, s3] where B is a boundary, the result
// is segments = [[s0, s1], [s2, s3]] and boundaries = [B]. The
// boundary atom itself appears in neither segment.
//
// Segment count is always len(boundaries)+1 when the pipeline is
// non-empty; either end segment may be empty (leading boundary, or
// trailing boundary).
func splitAtBoundaries(nodes []compiler.Expr) ([][]compiler.Expr, []BoundarySpec, error) {
	var (
		segments   [][]compiler.Expr
		boundaries []BoundarySpec
	)

	cur := make([]compiler.Expr, 0, len(nodes))

	for _, node := range nodes {
		name := atomName(node)
		if name == "" {
			cur = append(cur, node)
			continue
		}

		spec, isBoundary := BoundaryByName(name)
		if !isBoundary {
			cur = append(cur, node)
			continue
		}

		segments = append(segments, cur)
		boundaries = append(boundaries, spec)
		cur = make([]compiler.Expr, 0, len(nodes))
	}

	segments = append(segments, cur)

	return segments, boundaries, nil
}

// compileLinearUnary compiles a segment of the pipeline that is known
// to contain only unary nodes (atoms, projections, parallels, etc.).
//
// The segment is rebuilt into a PipelineExpr and handed to the normal
// AST compiler, so every existing code path — projections, dynamic
// atoms, parallel groups — continues to work unchanged.
func compileLinearUnary(
	nodes []compiler.Expr,
	reg *action.Registry,
	options *compileOptions,
) (*action.Builder[any, any], error) {
	if len(nodes) == 0 {
		return nil, xerr.Internal("flow: empty unary segment")
	}
	return compileAST(pipelineFromSlice(nodes), reg, options)
}
