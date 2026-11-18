package cli

import (
	"fmt"
	"strings"

	"github.com/nexssp/flow/compiler"
)

func legacyConstParamsWarnings(path string, ast compiler.Expr) []LintIssue {
	var issues []LintIssue
	var visit func(compiler.Expr)
	visit = func(expr compiler.Expr) {
		if expr == nil {
			return
		}
		if atom, ok := expr.(*compiler.Atom); ok {
			if message, found := legacyConstParamsWarning(atom); found {
				pos := atom.Pos
				file := pos.File
				if file == "" {
					file = path
				}
				issues = append(issues, LintIssue{
					File:    file,
					Line:    pos.Line,
					Column:  pos.Col,
					Kind:    lintIssueWarning,
					Message: message,
				})
			}
		}
		for _, child := range expr.Children() {
			visit(child)
		}
	}
	visit(ast)
	return issues
}

func legacyConstParamsWarning(atom *compiler.Atom) (string, bool) {
	if atom.Name != "runtime.const" {
		return "", false
	}

	_, hasValue := atom.Params["value"]
	_, hasVal := atom.Params["val"]
	if !hasValue && !hasVal {
		return "", false
	}

	keys := make([]string, 0, 3)
	for _, key := range []string{"value", "val", "coerce"} {
		if _, exists := atom.Params[key]; exists {
			keys = append(keys, "`"+key+"`")
		}
	}

	message := fmt.Sprintf("deprecated legacy const key=value scalar wrapper using %s; hint: for scalar output, replace the wrapper with the intended typed literal or reference; to return an object field, use colon syntax such as `const(value: 42)`. Check the old output type because legacy strings may be auto-parsed", strings.Join(keys, ", "))
	if hasValue && hasVal {
		message += "; `value` takes precedence over `val`"
	}
	if _, hasCoerce := atom.Params["coerce"]; hasCoerce {
		message += ". In key=value syntax, literal `coerce=true` is text, not a boolean; only a typed boolean true can coerce a preserved CLI/config/arg string, so do not translate it into a generic cast"
	}
	return message, true
}
