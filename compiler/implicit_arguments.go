package compiler

import "context"

// normalizeImplicitArgumentExpressions makes unquoted runtime values evaluate
// as expressions. Quoted strings stay literal, while schema-declared
// capability-reference fields and extension-normalized values retain their
// static syntax.
func normalizeImplicitArgumentExpressions(
	ctx context.Context,
	root Expr,
	schemas map[string][]ArgFieldSpec,
	normalizers []AtomValueNormalizeFunc,
) {
	walkExpr(root, func(node Expr) {
		atom, ok := node.(*Atom)
		if !ok {
			return
		}
		resolveSymbolsInAtom(ctx, atom)
		for _, normalize := range normalizers {
			if normalize != nil {
				normalize(atom)
			}
		}

		capabilityFields := make(map[string]ArgKind, len(schemas[atom.Name]))
		for _, field := range schemas[atom.Name] {
			capabilityFields[field.Name] = field.Kind
		}
		normalizeImplicitValue(atom.DirectInput)
		for key, value := range atom.Args {
			if isCapabilityRefKind(capabilityFields[key]) {
				continue
			}
			normalizeImplicitValue(value)
		}
		for _, value := range atom.ArgSpreads {
			normalizeImplicitValue(value)
		}
	})
}

func normalizeImplicitValue(value *Value) {
	if value == nil {
		return
	}
	switch value.Kind {
	case ValueBare:
		value.Kind = ValueExpression
		value.Expression = value.Str
		value.Str = ""
	case ValueMap:
		for _, entry := range value.Map {
			normalizeImplicitValue(entry.Value)
		}
	case ValueSlice:
		for _, item := range value.Slice {
			normalizeImplicitValue(item)
		}
	case ValueString, ValueNumber, ValueBool, ValueNull, ValueRef, ValueTyped, ValueExpression:
		// These values retain their parsed or normalized meaning.
	}
}
