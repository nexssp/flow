package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/require"
)

// runRequire generuje requires_gen.go na podstawie @require w .nflow.
//
//	nexssflow require <file.nflow> [-o out.go] [-pkg main]
//
// Domyślna ścieżka output: module root (katalog z go.mod), plik
// requires_gen.go. Walidujemy package name, bo generowany plik musi
// należeć do tego samego pakietu co main, aby init() odpaliło się
// przed cli.Run.
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
	_, meta, err := core.Preprocess(ctx, dt, string(src), path)
	if err != nil {
		return fatalf("preprocess: %v", err)
	}

	reqs, _ := meta["require"].([]require.Requirement)
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

// resolveOutputPath zwraca docelową ścieżkę pliku. Kolejność:
//  1. explicit -o
//  2. <module_root>/requires_gen.go (znajdź go.mod w górę od .nflow)
//  3. ./requires_gen.go (cwd fallback)
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

// runRequirePin uzupełnia brakujące wersje w pliku .nflow.
func runRequirePin(ctx context.Context, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: nexssflow require pin <file.nflow>")
		return 2
	}
	path := args[0]

	content, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read %s: %v", path, err)
	}

	lines := strings.Split(string(content), "\n")
	modified := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "@require") {
			continue
		}

		rawSpec := strings.TrimPrefix(trimmed, "@require")
		optsIdx := strings.Index(rawSpec, "{")
		specOnly := rawSpec
		trailing := ""
		if optsIdx >= 0 {
			specOnly = rawSpec[:optsIdx]
			trailing = " " + strings.TrimSpace(rawSpec[optsIdx:])
		}

		parts := strings.Fields(specOnly)
		if len(parts) != 1 ||
			strings.HasPrefix(parts[0], "./") ||
			strings.HasPrefix(parts[0], "../") {
			continue
		}

		modulePath := strings.Trim(parts[0], `"'`)

		ver, err := resolveLatestVersion(ctx, modulePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  could not resolve version for %s: %v\n",
				modulePath, err)
			continue
		}

		leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = fmt.Sprintf("%s@require %s %s%s",
			leadingSpace, modulePath, ver, trailing)
		modified = true
		fmt.Printf("✓ Pinned %s to %s\n", modulePath, ver)
	}

	if modified {
		//nolint:gosec // G703: path is the .nflow file the user asked to pin
		if err := os.WriteFile(path,
			[]byte(strings.Join(lines, "\n")), 0o600); err != nil {
			return fatalf("write %s: %v", path, err)
		}
		fmt.Printf("✓ Successfully updated %s\n", path)
	} else {
		fmt.Println("Everything up to date. No unpinned @require directives found.")
	}

	return 0
}

func resolveLatestVersion(ctx context.Context, modulePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", modulePath+"@latest")
	out, err := cmd.Output()
	if err != nil {
		cmd = exec.CommandContext(ctx, "go", "list", "-m", "-json", modulePath)
		out, err = cmd.Output()
		if err != nil {
			return "", err
		}
	}

	var res struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &res); err != nil || res.Version == "" {
		return "", errors.New("invalid metadata from go list")
	}

	return res.Version, nil
}
