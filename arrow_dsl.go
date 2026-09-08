package flow

import (
	"fmt"
	"strings"

	"github.com/nexssp/kernel/xerr"
)

// ParseArrowDSL converts a compact arrow pipeline expression into a standard GraphDefinition.
func ParseArrowDSL(name, dsl string) (GraphDefinition, error) {
	dsl = strings.TrimSpace(dsl)
	if dsl == "" {
		return GraphDefinition{}, xerr.BadRequest("graph: arrow DSL expression cannot be empty")
	}
	if name == "" {
		name = "dsl_pipeline"
	}

	stages := splitTopLevelAll(dsl, "->")
	if len(stages) == 0 {
		return GraphDefinition{}, xerr.BadRequest("graph: invalid pipeline syntax")
	}

	nodes := make([]NodeSpec, 0, len(stages))
	edges := make([]EdgeSpec, 0, len(stages)*2)
	var prevStageNodeIDs []string

	for stageIdx, stageRaw := range stages {
		stage := strings.TrimSpace(stageRaw)
		if stage == "" {
			return GraphDefinition{}, xerr.BadRequest(fmt.Sprintf("graph: empty stage at index %d", stageIdx))
		}

		var currentStageNodeIDs []string
		cleanStage := stripOuterParens(stage)
		parallelBranches := splitTopLevel(cleanStage, '&')

		if len(parallelBranches) > 1 {
			for branchIdx, branchRaw := range parallelBranches {
				branch := strings.TrimSpace(branchRaw)
				if branch == "" {
					continue
				}
				nodeID := fmt.Sprintf("step_%d_%d", stageIdx+1, branchIdx+1)
				nodeSpec := parseNodeSpec(nodeID, branch)
				nodes = append(nodes, nodeSpec)
				currentStageNodeIDs = append(currentStageNodeIDs, nodeID)
			}
		} else {
			nodeID := fmt.Sprintf("step_%d", stageIdx+1)
			nodeSpec := parseNodeSpec(nodeID, cleanStage)
			nodes = append(nodes, nodeSpec)
			currentStageNodeIDs = append(currentStageNodeIDs, nodeID)
		}

		if len(prevStageNodeIDs) > 0 {
			for _, fromID := range prevStageNodeIDs {
				for _, toID := range currentStageNodeIDs {
					edges = append(edges, EdgeSpec{
						From: fromID,
						To:   toID,
					})
				}
			}
		}

		prevStageNodeIDs = currentStageNodeIDs
	}

	return GraphDefinition{
		APIVersion: APIVersion,
		Kind:       "Graph",
		Metadata: Metadata{
			Name:    name,
			Version: "1.0.0",
		},
		Nodes: nodes,
		Edges: edges,
	}, nil
}

func parseNodeSpec(id, token string) NodeSpec {
	token = stripOuterParens(token)
	capability, params, bindings := parseTokenParamsAndBindings(token)

	return NodeSpec{
		ID:            id,
		Kind:          NodeTool,
		Capability:    capability,
		Params:        params,
		InputBindings: bindings,
	}
}

// parseTokenParamsAndBindings extracts parameters, modifiers (: # ~ @), and explicit
// field bindings (key=upstream.field) using fast-path indexing without regex allocations.
func parseTokenParamsAndBindings(token string) (string, map[string]any, map[string]string) {
	params := make(map[string]any)
	bindings := make(map[string]string)

	// 1. Extract explicit bindings: "node(code=pack.content, val=123)"
	if start := strings.IndexByte(token, '('); start != -1 {
		if end := strings.LastIndexByte(token, ')'); end > start {
			rawBindings := token[start+1 : end]
			token = strings.TrimSpace(token[:start] + token[end+1:])

			for _, pair := range strings.Split(rawBindings, ",") {
				pair = strings.TrimSpace(pair)
				if key, val, ok := strings.Cut(pair, "="); ok {
					key = strings.TrimSpace(key)
					val = strings.TrimSpace(val)
					if strings.Contains(val, ".") {
						bindings[key] = val
					} else {
						params[key] = val
					}
				}
			}
		}
	}

	// 2. Extract prompt (@)
	if idx := strings.IndexByte(token, '@'); idx != -1 {
		params["prompt"] = strings.TrimSpace(token[idx+1:])
		token = token[:idx]
	}

	// 3. Extract exclusions (~)
	if idx := strings.IndexByte(token, '~'); idx != -1 {
		exPart := token[idx+1:]
		token = token[:idx]
		parts := strings.Split(exPart, ",")
		excludes := make([]string, 0, len(parts))
		for _, ex := range parts {
			if t := strings.TrimSpace(ex); t != "" {
				excludes = append(excludes, t)
			}
		}
		if len(excludes) > 0 {
			params["excludes"] = excludes
		}
	}

	// 4. Extract target (#)
	if idx := strings.IndexByte(token, '#'); idx != -1 {
		params["targets"] = []string{strings.TrimSpace(token[idx+1:])}
		token = token[:idx]
	}

	// 5. Extract profile (:)
	if idx := strings.IndexByte(token, ':'); idx != -1 {
		params["profile"] = strings.TrimSpace(token[idx+1:])
		token = token[:idx]
	}

	return strings.TrimSpace(token), params, bindings
}
