package flow

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/dslparse"
	"github.com/nexssp/kernel/xerr"
)

// ParseArrowDSL converts an Arrow DSL expression into a
// GraphDefinition. It runs the compiler's parser, then walks the
// resulting AST and produces one NodeSpec per atom, one EdgeSpec per
// arrow.
//
// Modifiers attached to an atom are parsed a second time here (the
// parser already collected them as raw strings) so that a small,
// well-defined subset can be lifted onto NodeSpec fields: effect,
// approval, timeout, retry. Everything else stays in the atom and is
// applied at compile time by resolveDynamicNode.
func ParseArrowDSL(name, dsl string) (GraphDefinition, error) {
	dsl = SanitizeDSL(dsl)
	if strings.TrimSpace(dsl) == "" {
		return GraphDefinition{}, xerr.BadRequest("graph: arrow DSL expression cannot be empty")
	}

	if name == "" {
		name = "dsl_pipeline"
	}

	parser := compiler.NewParserWithFile(dsl, name)

	ast, err := parser.ParseExpression()
	if err != nil {
		return GraphDefinition{}, err
	}

	def := GraphDefinition{
		APIVersion: APIVersion,
		Kind:       "Graph",
		Metadata: Metadata{
			Name:    name,
			Version: "1.0.0",
		},
		Nodes: []NodeSpec{},
		Edges: []EdgeSpec{},
	}

	nodeCounts := make(map[string]int)

	var walk func(expr compiler.Expr, prevIDs []string) ([]string, error)

	walk = func(expr compiler.Expr, prevIDs []string) ([]string, error) {
		switch n := expr.(type) {
		case *compiler.PipelineExpr:
			leftIDs, walkErr := walk(n.Left, prevIDs)
			if walkErr != nil {
				return nil, walkErr
			}
			return walk(n.Right, leftIDs)

		case *compiler.ParallelExpr:
			var outIDs []string

			for _, child := range n.Children {
				childIDs, walkErr := walk(child, prevIDs)
				if walkErr != nil {
					return nil, walkErr
				}
				outIDs = append(outIDs, childIDs...)
			}
			return outIDs, nil

		case *compiler.AtomExpr:
			if len(n.Args) > 0 {
				return nil, &compiler.NeedsPipelineError{
					Reason: fmt.Sprintf(
						"atom %q carries @{ } args; pipeline compiler required",
						n.Name,
					),
				}
			}

			count := nodeCounts[n.Name]
			nodeCounts[n.Name]++

			nodeID := n.Name
			if count > 0 {
				nodeID = fmt.Sprintf("%s_%d", n.Name, count)
			}

			params := make(map[string]any)
			for k, v := range n.Params {
				params[k] = v
			}
			if n.Prompt != "" {
				params["prompt"] = n.Prompt
			}
			if len(n.Excludes) > 0 {
				params["excludes"] = n.Excludes
			}
			if len(n.Targets) > 0 {
				params["targets"] = n.Targets
			}
			if n.Profile != "" {
				params["profile"] = n.Profile
			}

			spec := NodeSpec{
				ID:            nodeID,
				Kind:          NodeTool,
				Capability:    n.Name,
				Params:        params,
				InputBindings: n.Inputs,
			}

			// Lift the well-defined modifier subset onto NodeSpec.
			applyAtomModifiers(n, &spec)

			def.Nodes = append(def.Nodes, spec)

			for _, pID := range prevIDs {
				def.Edges = append(def.Edges, EdgeSpec{
					From: pID,
					To:   nodeID,
				})
			}
			return []string{nodeID}, nil

		case *compiler.ProjectionExpr:
			count := nodeCounts["projection"]
			nodeCounts["projection"]++

			nodeID := "projection"
			if count > 0 {
				nodeID = fmt.Sprintf("projection_%d", count)
			}

			spec := NodeSpec{
				ID:         nodeID,
				Kind:       NodeTool,
				Capability: "{ " + n.Raw + " }",
			}
			def.Nodes = append(def.Nodes, spec)

			for _, pID := range prevIDs {
				def.Edges = append(def.Edges, EdgeSpec{
					From: pID,
					To:   nodeID,
				})
			}
			return []string{nodeID}, nil

		case *compiler.LoopExpr, *compiler.ConditionalExpr, *compiler.FallbackExpr:
			return nil, &compiler.NeedsPipelineError{
				Reason: fmt.Sprintf(
					"DSL contains %T, which needs the pipeline compiler (loop, conditional, and fallback are runtime constructs, not DAG topology)",
					n,
				),
			}

		default:
			return nil, xerr.BadRequest(fmt.Sprintf(
				"graph: AST node type %T not supported in static DAG manifest", n))
		}
	}

	_, err = walk(ast, nil)
	if err != nil {
		return GraphDefinition{}, err
	}

	return def, nil
}

// applyAtomModifiers reads the modifier list already collected by the
// parser and copies the subset that belongs on NodeSpec.
//
// Modifiers that are not lifted here — cache, coalesce, dedup, rate
// limit, roles, transports, provider, model, typed, system — remain on
// the atom and are applied at compile time by resolveDynamicNode. The
// split is deliberate: NodeSpec fields are visible to the DAG
// scheduler, everything else is a runtime concern of a single node.
func applyAtomModifiers(atom *compiler.AtomExpr, spec *NodeSpec) {
	if atom == nil || spec == nil || len(atom.Modifiers) == 0 {
		return
	}

	line := atom.Name + ":" + strings.Join(atom.Modifiers, ":")
	parsed, ok := dslparse.ParseLine(line)
	if !ok {
		return
	}
	mod := parsed.Modifiers

	if mod.Effect != "" {
		spec.Effect = EffectClass(mod.Effect)
	}

	// :gate= on an atom forces approval for this node, independently
	// of the file-level @gate rules. File-level rules are merged in
	// applyGates at the start of Compile.
	if mod.Gate != "" {
		spec.Approval = true
	}

	if mod.Timeout > 0 {
		spec.TimeoutMS = mod.Timeout.Milliseconds()
	}

	if mod.RetryMax > 0 {
		spec.Retry.MaxAttempts = mod.RetryMax
	}
}
