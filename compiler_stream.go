package flow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

type pipelineMode uint8

const (
	pipelineModeUnary pipelineMode = iota
	pipelineModeStream
	// pipelineModeSegment is selected when the pipeline contains at
	// least one stream boundary (see boundary.go). It triggers the
	// segmented compiler in compiler_segment.go instead of the
	// single-mode compilers.
	pipelineModeSegment
)

// detectPipelineMode inspects the AST and decides which compiler
// should handle it.
//
// Precedence:
//
//  1. If any atom is a boundary (e.g. `collect`), the pipeline is
//     segmented. This takes priority over the stream check because a
//     boundary requires the special two-mode compilation path.
//  2. Otherwise, if any atom is registered as a stream source or
//     stream operator, the pipeline is pure stream.
//  3. Otherwise, the pipeline is a plain unary chain.
func detectPipelineMode(ast compiler.Expr, registry *Registry) pipelineMode {
	var (
		hasBoundary bool
		hasStream   bool
	)

	walkAtoms(ast, func(name string) {
		if _, ok := BoundaryByName(name); ok {
			hasBoundary = true
			return
		}
		if registry == nil {
			return
		}
		kind, ok := registry.Resolve(name)
		if ok && (kind == KindSource || kind == KindOperator) {
			hasStream = true
		}
	})

	switch {
	case hasBoundary:
		return pipelineModeSegment
	case hasStream:
		return pipelineModeStream
	default:
		return pipelineModeUnary
	}
}

// walkAtoms visits every AtomExpr in the expression tree in source
// order. Projections, parallels, conditionals, and loops are all
// traversed. The visit function receives the raw atom name.
func walkAtoms(node compiler.Expr, visit func(string)) {
	switch expr := node.(type) {
	case nil:
		return
	case *compiler.AtomExpr:
		visit(expr.Name)
	case *compiler.PipelineExpr:
		walkAtoms(expr.Left, visit)
		walkAtoms(expr.Right, visit)
	case *compiler.ParallelExpr:
		for _, child := range expr.Children {
			walkAtoms(child, visit)
		}
	case *compiler.FallbackExpr:
		walkAtoms(expr.Left, visit)
		walkAtoms(expr.Right, visit)
	case *compiler.ConditionalExpr:
		walkAtoms(expr.Gate, visit)
		walkAtoms(expr.Target, visit)
		walkAtoms(expr.Else, visit)
	case *compiler.LoopExpr:
		walkAtoms(expr.Body, visit)
	}
}

// buildStreamSource compiles a pure stream pipeline into a
// pipelineStreamAction that owns the source and its operators. The
// action is not wrapped or drained: callers decide what to do with it.
//
// This separation lets the segmented compiler reuse the same
// stream-construction logic as the pure-stream compiler, without
// forcing the stream to be drained to a counter or to a slice.
func buildStreamSource(
	ast compiler.Expr,
	options *compileOptions,
) (*pipelineStreamAction, error) {
	head, operators, err := decomposeStreamPipeline(ast, options.flowRegistry, options)
	if err != nil {
		return nil, err
	}

	var composed StreamOperator
	if len(operators) == 0 {
		composed = identityOperator{}
	} else {
		composed, err = ComposeOperators(operators...)
		if err != nil {
			return nil, err
		}
	}

	return &pipelineStreamAction{
		meta:   &action.Meta{Name: head.Describe().Name},
		source: head,
		ops:    composed,
	}, nil
}

// compileStreamPipeline compiles a pure stream pipeline and drains it
// to a single summary value (currently a count of items). This is the
// fallback for pipelines that use stream sources without a boundary.
func compileStreamPipeline(
	ast compiler.Expr,
	options *compileOptions,
) (*action.Builder[any, any], error) {
	src, err := buildStreamSource(ast, options)
	if err != nil {
		return nil, err
	}
	return action.Dynamic(streamAsUnary(src)), nil
}

// ─── Existing helpers below this line are unchanged ─────────────────────────

func decomposeStreamPipeline(
	ast compiler.Expr,
	registry *Registry,
	options *compileOptions,
) (action.AnyStreamAction, []StreamOperator, error) {
	switch expr := ast.(type) {
	case *compiler.AtomExpr:
		head, err := resolveSourceAtom(expr, registry, options)
		return head, nil, err

	case *compiler.PipelineExpr:
		head, operators, err := decomposeStreamPipeline(expr.Left, registry, options)
		if err != nil {
			return nil, nil, err
		}
		operator, err := resolveOperatorAtom(expr.Right, registry, options)
		if err != nil {
			return nil, nil, err
		}
		return head, append(operators, operator), nil

	default:
		return nil, nil, fmt.Errorf("flow: stream pipeline only supports '->' chains of atoms, got %T", ast)
	}
}

func resolveSourceAtom(node compiler.Expr, registry *Registry, options *compileOptions) (action.AnyStreamAction, error) {
	atom, ok := node.(*compiler.AtomExpr)
	if !ok {
		return nil, fmt.Errorf("flow: stream pipeline head must be an atom, got %T", node)
	}
	if registry == nil {
		return nil, fmt.Errorf("flow: cannot resolve atom %q: no flow registry configured", atom.Name)
	}
	kind, ok := registry.Resolve(atom.Name)
	if !ok {
		return nil, fmt.Errorf("flow: atom %q is not registered", atom.Name)
	}
	switch kind {
	case KindSource:
		source, _ := registry.GetStream(atom.Name)
		if len(atom.Modifiers) > 0 {
			modifiersMap := parseAtomModifiersToMap(atom.Modifiers, options)
			return &configuredStreamSource{
				AnyStreamAction: source,
				compileConfig:   modifiersMap,
			}, nil
		}
		return source, nil

	case KindOperator:
		return nil, fmt.Errorf("flow: operator %q cannot start a pipeline; start with a registered source", atom.Name)
	case KindUnary:
		return nil, fmt.Errorf("flow: unary action %q cannot be the head of a stream pipeline", atom.Name)
	}
	return nil, fmt.Errorf("flow: unknown kind for atom %q", atom.Name)
}

func resolveOperatorAtom(node compiler.Expr, registry *Registry, options *compileOptions) (StreamOperator, error) {
	atom, ok := node.(*compiler.AtomExpr)
	if !ok {
		return nil, fmt.Errorf("flow: operator position must be an atom, got %T", node)
	}
	if registry == nil {
		return nil, fmt.Errorf("flow: cannot resolve atom %q: no flow registry configured", atom.Name)
	}
	kind, ok := registry.Resolve(atom.Name)
	if !ok {
		return nil, fmt.Errorf("flow: atom %q is not registered", atom.Name)
	}
	switch kind {
	case KindOperator:
		named, _ := registry.GetOperator(atom.Name)
		parameters := parseAtomModifiersToMap(atom.Modifiers, options)
		return named.Build(parameters)

	case KindSource:
		return nil, fmt.Errorf("flow: source %q cannot appear after '->'", atom.Name)
	case KindUnary:
		return nil, fmt.Errorf("flow: unary action %q cannot follow a stream source or operator", atom.Name)
	}
	return nil, fmt.Errorf("flow: unknown kind for atom %q", atom.Name)
}

// parseAtomModifiersToMap is unchanged.
func parseAtomModifiersToMap(modifiers []string, options *compileOptions) map[string]any {
	if len(modifiers) == 0 {
		return nil
	}
	out := make(map[string]any, len(modifiers))

	for _, rawModifier := range modifiers {
		equalIndex := strings.IndexByte(rawModifier, '=')
		if equalIndex < 0 {
			out[strings.ToLower(rawModifier)] = true
			continue
		}
		key := strings.ToLower(strings.TrimSpace(rawModifier[:equalIndex]))
		rawValue := strings.Trim(rawModifier[equalIndex+1:], `"'`)
		resolvedVal := ResolveParamRef(rawValue, options)

		if key == "dirs" {
			if strVal, ok := resolvedVal.(string); ok {
				switch {
				case strings.HasPrefix(strVal, "[") && strings.HasSuffix(strVal, "]"):
					var list []string
					if err := json.Unmarshal([]byte(strVal), &list); err == nil {
						resolvedVal = list
					} else {
						resolvedVal = []string{strVal}
					}
				case strings.Contains(strVal, ","):
					var list []string
					for _, p := range strings.Split(strVal, ",") {
						if p = strings.TrimSpace(p); p != "" {
							list = append(list, p)
						}
					}
					resolvedVal = list
				default:
					resolvedVal = []string{strVal}
				}
			}
		}

		out[key] = resolvedVal
	}
	return out
}
