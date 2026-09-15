package flow

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/xerr"
)

func ParseArrowDSL(name, dsl string) (GraphDefinition, error) {
	dsl = SanitizeDSL(dsl)
	if strings.TrimSpace(dsl) == "" {
		return GraphDefinition{}, xerr.BadRequest("graph: arrow DSL expression cannot be empty")
	}

	if name == "" {
		name = "dsl_pipeline"
	}

	parser := compiler.NewParser(dsl)

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

		default:
			return nil, xerr.BadRequest(fmt.Sprintf("graph: AST node type %T not supported in static DAG manifest", n))
		}
	}

	_, err = walk(ast, nil)
	if err != nil {
		return GraphDefinition{}, err
	}

	return def, nil
}
