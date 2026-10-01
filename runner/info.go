package runner

import (
	"fmt"
	"io"
	"strings"

	"github.com/nexssp/flow/core"
)

// PrintInfo describes a .nflow file without executing it. It shows
// directives from meta, the sequence of atoms from the AST, and expected input.
func PrintInfo(w io.Writer, path, _ string, meta map[string]any, ast core.Expr) int {
	_, _ = fmt.Fprintf(w, "\n📋 FLOW INSPECTION: %s\n", path)
	_, _ = fmt.Fprintln(w, strings.Repeat("═", 60))

	if desc, ok := meta["description"].(string); ok && desc != "" {
		_, _ = fmt.Fprintf(w, "Description: %s\n", desc)
	}

	if asserts, ok := meta["asserts"].([]string); ok && len(asserts) > 0 {
		_, _ = fmt.Fprintf(w, "Asserts: %d\n", len(asserts))
		for _, a := range asserts {
			_, _ = fmt.Fprintf(w, "  • %s\n", a)
		}
	}

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "⚡ Pipeline:")

	names := core.AtomNames(ast)
	for i, n := range names {
		_, _ = fmt.Fprintf(w, "  [%d] %s\n", i+1, n)
	}

	_, _ = fmt.Fprintln(w, strings.Repeat("═", 60))
	return 0
}
