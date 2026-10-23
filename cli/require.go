package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
)

// runRequire generates requires_gen.go from @require declarations in a .nflow file.
//
//	nexssflow require <file.nflow> [-o out.go] [-pkg main]
//
// By default, it writes to the module root. Use the executable package
// (main by default) so the generated init runs before cli.Run.
func runRequire(ctx context.Context, inv *invocation, args []string) int {
	fs := flag.NewFlagSet("require", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	out := fs.String("o", "", "output file (default: <module_root>/requires_gen.go)")
	pkg := fs.String("pkg", "main", "package name for generated file")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	positional := fs.Args()
	if len(positional) < 1 {
		fmt.Fprintln(os.Stderr,
			"usage: nexssflow require <file.nflow> [-o out.go] [-pkg main]")
		return 2
	}
	path := positional[0]

	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read: %v", err)
	}

	dt, err := inv.directiveTable()
	if err != nil {
		return fatalf("directives: %v", err)
	}
	_, meta, err := compiler.Preprocess(ctx, dt, string(src), path)
	if err != nil {
		return fatalf("preprocess: %v", err)
	}

	reqs, reqErr := require.SourceRequirements(meta)
	if reqErr != nil {
		return fatalf("requires: %v", reqErr)
	}
	if len(reqs) == 0 {
		fmt.Fprintln(os.Stderr, "no @require declarations found in", path)
		return 0
	}

	resolved, err := require.Resolve(require.Plan{Modules: reqs}, "")
	if err != nil {
		return fatalf("resolve: %v", err)
	}

	outPath, err := resolveOutputPath(*out, path)
	if err != nil {
		return fatalf("%v", err)
	}

	var buf bytes.Buffer
	if err := require.GenerateGlue(&buf, *pkg, "", resolved); err != nil {
		return fatalf("generate: %v", err)
	}

	if err := os.WriteFile(outPath, buf.Bytes(), 0o600); err != nil {
		return fatalf("write: %v", err)
	}

	fmt.Fprintf(os.Stdout, "✓ %s (%d modules)\n", outPath, len(resolved))
	for i := range resolved {
		m := &resolved[i]
		if m.Requirement.IsLocal() {
			fmt.Fprintf(os.Stdout, "  • %s (local: %s)\n", m.Requirement.Import, m.Dir)
		} else {
			fmt.Fprintf(os.Stdout, "  • %s@%s\n", m.Requirement.Import, m.Requirement.Version)
		}
	}
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout,
		"→ wygenerowany harness wywoła eksportowane Bundle(nil) bez zależności od import-path registry.")
	return 0
}

// resolveOutputPath checks, in order: explicit -o, the module root, then the current directory.
func resolveOutputPath(explicit, nflowPath string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	abs, err := filepath.Abs(nflowPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", nflowPath, err)
	}

	root := findModuleRoot(filepath.Dir(abs))
	if root == "" {
		return "requires_gen.go", nil
	}
	return filepath.Join(root, "requires_gen.go"), nil
}

func findModuleRoot(dir string) string {
	curr := dir
	for {
		if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return ""
		}
		curr = parent
	}
}
