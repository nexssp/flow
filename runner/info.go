package runner

import (
	"encoding/json"
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

type InfoShape struct {
	Path        string   `json:"path"`
	Description string   `json:"description,omitempty"`
	Asserts     []string `json:"asserts,omitempty"`
	Atoms       []string `json:"atoms"`
}

func PrintInfoJSON(w io.Writer, path string, meta map[string]any, ast core.Expr) error {
	shape := InfoShape{
		Path:  path,
		Atoms: core.AtomNames(ast),
	}
	if desc, ok := meta["description"].(string); ok {
		shape.Description = desc
	}
	if asserts, ok := meta["asserts"].([]string); ok {
		shape.Asserts = asserts
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(shape)
}
