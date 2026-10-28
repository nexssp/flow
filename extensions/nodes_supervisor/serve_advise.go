package nodes_supervisor

import (
	"strings"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/compiler"
)

// adviseServe validates the literal service graph before the AST is lowered.
// Runtime validation remains in place for values supplied through typed calls.
func adviseServe(atom *compiler.Atom, _ *action.Builder[any, any]) error {
	if atom.Name != "supervisor.serve" {
		return nil
	}
	for key := range atom.Args {
		if key != "services" {
			return compiler.SourceError(atom.Pos, "supervisor.serve: unknown argument %q", key)
		}
	}

	nodes, err := literalServiceNodes(atom)
	if err != nil {
		return err
	}
	if _, err := buildServicePlan(nodes); err != nil {
		return compiler.SourceError(atom.Pos, "supervisor.serve: %s", err)
	}
	return nil
}

func literalServiceNodes(atom *compiler.Atom) ([]serviceNode, error) {
	value, ok := atom.Args["services"]
	if !ok || value == nil || value.Kind != compiler.ValueSlice || len(value.Slice) == 0 {
		return nil, compiler.SourceError(atom.Pos, "supervisor.serve: 'services' must be a non-empty literal list")
	}

	nodes := make([]serviceNode, len(value.Slice))
	for i, item := range value.Slice {
		node, err := literalServiceNode(atom, i, item)
		if err != nil {
			return nil, err
		}
		nodes[i] = node
	}
	return nodes, nil
}

func literalServiceNode(atom *compiler.Atom, index int, value *compiler.Value) (serviceNode, error) {
	if value == nil || value.Kind != compiler.ValueMap {
		return serviceNode{}, compiler.SourceError(atom.Pos, "supervisor.serve: services[%d] must be an object", index)
	}

	node := serviceNode{enabled: true}
	for _, field := range value.Map {
		if err := setServiceNodeField(atom, index, &node, field.Key, field.Value); err != nil {
			return serviceNode{}, err
		}
	}
	return node, nil
}

func setServiceNodeField(atom *compiler.Atom, index int, node *serviceNode, key string, value *compiler.Value) error {
	switch key {
	case "id":
		id, valid := staticString(value, false)
		if !valid {
			return compiler.SourceError(atom.Pos, "supervisor.serve: services[%d].id must be a quoted string", index)
		}
		node.id = id
	case "action":
		name, valid := staticString(value, true)
		if !valid {
			return compiler.SourceError(atom.Pos, "supervisor.serve: services[%d].action must be a literal action name", index)
		}
		node.action = name
	case "depends_on":
		dependencies, err := literalDependencies(atom, index, value)
		if err != nil {
			return err
		}
		node.dependsOn = dependencies
	case "enabled":
		enabled, err := literalEnabled(atom, index, value)
		if err != nil {
			return err
		}
		node.enabled = enabled
	case "payload":
		// Payload is intentionally opaque to the supervisor.
	default:
		return compiler.SourceError(atom.Pos, "supervisor.serve: services[%d] has unknown field %q", index, key)
	}
	return nil
}

func literalDependencies(atom *compiler.Atom, index int, value *compiler.Value) ([]string, error) {
	if value == nil || value.Kind != compiler.ValueSlice {
		return nil, compiler.SourceError(atom.Pos, "supervisor.serve: services[%d].depends_on must be a list of literal service ids", index)
	}

	dependencies := make([]string, 0, len(value.Slice))
	for i, dependency := range value.Slice {
		id, valid := staticString(dependency, true)
		if !valid {
			return nil, compiler.SourceError(atom.Pos, "supervisor.serve: services[%d].depends_on[%d] must be a literal service id", index, i)
		}
		dependencies = append(dependencies, id)
	}
	return dependencies, nil
}

func literalEnabled(atom *compiler.Atom, index int, value *compiler.Value) (bool, error) {
	if value != nil && value.Kind == compiler.ValueBool {
		return value.Bool, nil
	}
	if value != nil && value.Kind == compiler.ValueRef {
		// Runtime references are resolved from the pipeline input.
		return true, nil
	}
	return false, compiler.SourceError(atom.Pos, "supervisor.serve: services[%d].enabled must be a boolean or runtime reference", index)
}

func staticString(value *compiler.Value, allowBare bool) (string, bool) {
	if value == nil {
		return "", false
	}
	if value.Kind == compiler.ValueString || (allowBare && value.Kind == compiler.ValueBare) {
		return strings.TrimSpace(value.Str), true
	}
	return "", false
}
