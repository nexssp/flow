// Package runner provides the Flow runtime: it compiles, executes, and
// observes pipelines. It never downloads modules and never generates
// code at runtime. Build-time validation is a separate concern handled
// by the code generator in cmd/nexssflow.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/nexssp/flow/directives"
	"github.com/nexssp/kernel/xerr"
)

// RequiresPlan is the validated output of AnalyzeRequires.
type RequiresPlan struct {
	Modules []RequiredModule
}

// RequiredModule is one @require declaration after validation.
type RequiredModule struct {
	Import  string
	Version string
	Local   bool
}

// ModuleAllowlist maps "import@version" to true. The application
// supplies this at build time. An empty allowlist is a fail-closed
// policy: no @require is permitted.
type ModuleAllowlist map[string]bool

// AnalyzeRequires validates every @require declaration against the
// allowlist. Returns xerr.NotFound for any unlisted module. Never
// downloads, compiles, or executes anything.
func AnalyzeRequires(pre *directives.Preprocessed, allow ModuleAllowlist) (*RequiresPlan, error) {
	if pre == nil {
		return &RequiresPlan{}, nil
	}

	plan := &RequiresPlan{Modules: make([]RequiredModule, 0, len(pre.Requires))}

	for _, r := range pre.Requires {
		key := r.Import
		if r.Version != "" {
			key = r.Import + "@" + r.Version
		}

		if !allow[key] {
			return nil, xerr.NotFound(fmt.Sprintf(
				"@require %s: module is not in the allowlist "+
					"(add it to the build policy or remove the directive)",
				key,
			))
		}

		plan.Modules = append(plan.Modules, RequiredModule{
			Import:  r.Import,
			Version: r.Version,
			Local:   r.IsLocal(),
		})
	}

	sort.Slice(plan.Modules, func(i, j int) bool {
		return plan.Modules[i].Import < plan.Modules[j].Import
	})

	return plan, nil
}

// ManifestHash returns a stable identifier of the @require list and its
// options. The code generator stores this hash in requires_gen.go. On
// every build, if the computed hash differs from the stored one, the
// generator regenerates the file. If it matches, no work is done.
//
// opts maps importPath to its option map.
func ManifestHash(modules []RequiredModule, opts map[string]map[string]string) string {
	h := sha256.New()

	sorted := append([]RequiredModule(nil), modules...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Import != sorted[j].Import {
			return sorted[i].Import < sorted[j].Import
		}
		return sorted[i].Version < sorted[j].Version
	})

	for _, m := range sorted {
		fmt.Fprintf(h, "module %s@%s\n", m.Import, m.Version)
		keys := make([]string, 0, len(opts[m.Import]))
		for k := range opts[m.Import] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(h, "  %s=%s\n", k, opts[m.Import][k])
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// ParseOptions extracts the { key: "value", ... } block attached to a
// @require declaration. Returns a copy of the map so callers cannot
// mutate the parsed representation.
func ParseOptions(pre *directives.Preprocessed, importPath string) map[string]string {
	if pre == nil {
		return map[string]string{}
	}
	for _, requirement := range pre.Requires {
		if requirement.Import == importPath {
			if len(requirement.Options) > 0 {
				optionsCopy := make(map[string]string, len(requirement.Options))
				for key, value := range requirement.Options {
					optionsCopy[key] = value
				}
				return optionsCopy
			}
			return map[string]string{}
		}
	}
	return map[string]string{}
}

// ResolveRequires weryfikuje deklaracje @require i zwraca ścieżkę do skompilowanej binarki.
func ResolveRequires(ctx context.Context, reqs []directives.Requirement, stdout, stderr io.Writer) (string, error) {
	if len(reqs) == 0 {
		return "", errors.New("brak deklaracji @require")
	}

	cacheDir := filepath.Join(".nexss", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("tworzenie cache: %w", err)
	}

	// Unikalny hash ze wszystkich modułów w @require
	h := sha256.New()
	for _, r := range reqs {
		fmt.Fprintf(h, "%s@%s\n", r.Import, r.Version)
	}
	hash := hex.EncodeToString(h.Sum(nil))[:16]

	binName := "harness_" + hash
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(cacheDir, binName)

	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil // Trafienie w cache
	}

	// Jeśli nie ma w cache, budujemy bieżący projekt
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kompilacja harnessu nie powiodła się: %w", err)
	}

	return binPath, nil
}
