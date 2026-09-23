package flow

import "github.com/nexssp/flow/compiler"

// HasStreamAtoms reports whether dsl references any atom the flow
// registry knows as a stream source or stream operator.
//
// Stream pipelines look like ordinary atom chains to ParseArrowDSL —
// they parse into a DAG of nodes with no edges and no loop, no
// conditional, no fallback. But stream atoms are not unary actions;
// resolving fs.walk through the DAG compiler fails because fs.walk is
// a source, not an action. This check lets the executor route stream
// pipelines through the pipeline compiler before the DAG compiler
// ever sees them.
//
// A nil or empty registry returns false.
func HasStreamAtoms(dsl string, registry *Registry) bool {
	if registry == nil {
		return false
	}

	parser := compiler.NewParser(dsl)
	ast, err := parser.ParseExpression()
	if err != nil {
		return false
	}

	found := false
	walkAtoms(ast, func(name string) {
		if found {
			return
		}
		kind, ok := registry.Resolve(name)
		if !ok {
			return
		}
		if kind == KindSource || kind == KindOperator {
			found = true
		}
	})
	return found
}
