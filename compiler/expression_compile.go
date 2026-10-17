package compiler

import (
	"fmt"
	"slices"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/checker"
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/vm"
)

// CompileExpression compiles expr-lang source with the caller's options. Callers
// apply PreprocessDots first when accepting Flow's input-dot syntax. On failure,
// bare identifiers actually resolved to builtins get an actionable payload-field
// hint. Calls, member names, strings, disabled builtins and overrides are not
// guessed from substrings, and successful expressions are never rejected.
func CompileExpression(source string, options ...expr.Option) (*vm.Program, error) {
	var config *conf.Config
	opts := append(slices.Clone(options), func(c *conf.Config) { config = c })
	program, err := expr.Compile(source, opts...)
	if err == nil {
		return program, nil
	}
	// Inspect only failed expressions, using the exact configuration expr used.
	// ParseCheck returns its typed tree even on a type error. This avoids an AST
	// patcher and the extra checking pass it would impose on successful builds.
	tree, checkErr := checker.ParseCheck(source, config)
	if checkErr == nil || tree == nil || tree.Node == nil {
		return nil, err
	}
	uses := make(map[*ast.IdentifierNode]string)
	ast.Find(tree.Node, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.IdentifierNode:
			index, exists := builtin.Index[n.Value]
			if exists && n.Nature().TypeData != nil && n.Nature().Func == builtin.Builtins[index] {
				uses[n] = n.Value
			}
		case *ast.CallNode:
			if callee, ok := n.Callee.(*ast.IdentifierNode); ok {
				delete(uses, callee)
			}
		}
		return false
	})
	var names []string
	for _, name := range uses {
		names = append(names, name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) == 0 {
		return nil, err
	}
	var hints []string
	for _, name := range names {
		hints = append(hints, fmt.Sprintf("%q resolves to a built-in expression function; for a payload field use .%s", name, name))
	}
	return nil, fmt.Errorf("%w; %s", err, strings.Join(hints, "; "))
}
