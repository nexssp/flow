package require

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/nexssp/flow/core"
)

type Requirement struct {
	Import string
	// PackagePath is the Go import path selected for the bundle package.
	// It may differ from Import when a bare repository target defaults to
	// its nexssflow package or when Go resolves a package to a nested module.
	PackagePath string
	Alias       string
	Version     string
	// ResolvedVersion is the provider module version selected by Go for a
	// versioned remote bundle package. Local and workspace replacements may
	// leave it empty.
	ResolvedVersion string
	LocalPath       string
	ModuleRoot      string
	ModulePath      string
	IsLoose         bool
	LooseID         string
	Options         map[string]string
}

func (r Requirement) IsLocal() bool { return r.LocalPath != "" }

func FromMeta(meta map[string]any) []Requirement {
	reqs, _ := meta["require"].([]Requirement)
	return reqs
}

type Plan struct {
	Modules []Requirement
}

type ResolvedModule struct {
	Requirement Requirement
	Dir         string
	GoMod       string
}

func Parse(spec, baseDir string, opts map[string]string, file string, line int) (Requirement, error) {
	parts := strings.Fields(spec)
	if len(parts) == 0 {
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require: missing module target")
	}

	importPath := strings.Trim(parts[0], `"'`)
	parts = parts[1:]

	version := ""
	alias := ""
	for len(parts) > 0 {
		if parts[0] == "as" {
			if alias != "" {
				return Requirement{}, core.SourceError(
					core.Position{File: file, Line: line},
					"@require: only one local namespace qualifier is allowed")
			}
			if len(parts) < 2 {
				return Requirement{}, core.SourceError(
					core.Position{File: file, Line: line},
					"@require: missing local namespace qualifier after `as`")
			}
			alias = strings.Trim(parts[1], `"'`)
			if !validNamespaceQualifier(alias) {
				return Requirement{}, core.SourceError(
					core.Position{File: file, Line: line},
					"@require: invalid local namespace qualifier %q", alias)
			}
			parts = parts[2:]
			continue
		}
		if version != "" {
			return Requirement{}, core.SourceError(
				core.Position{File: file, Line: line},
				"@require: extraneous tokens after declaration: %v", parts)
		}
		version = strings.Trim(parts[0], `"'`)
		if !strings.HasPrefix(version, "v") && !isLocal(importPath) {
			return Requirement{}, core.SourceError(
				core.Position{File: file, Line: line},
				"@require: remote @require needs a version: `@require %s v1.2.3`", importPath)
		}
		parts = parts[1:]
	}

	if isLocal(importPath) {
		if version != "" {
			return Requirement{}, core.SourceError(
				core.Position{File: file, Line: line},
				"@require: local path must not carry a version: %q", importPath)
		}
		req, err := resolveLocal(importPath, baseDir, opts, file, line)
		if err != nil {
			return Requirement{}, err
		}
		req.Alias = alias
		return req, nil
	}

	return Requirement{
		Import:  importPath,
		Alias:   alias,
		Version: version,
		Options: opts,
	}, nil
}

func isLocal(s string) bool {
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, `.\`) ||
		strings.HasPrefix(s, `..\`) ||
		filepath.IsAbs(s)
}

func validNamespaceQualifier(alias string) bool {
	if alias == "" || alias == "_" || token.Lookup(alias).IsKeyword() {
		return false
	}
	for i, r := range alias {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func resolveLocal(name, baseDir string, opts map[string]string, file string, line int) (Requirement, error) {
	if baseDir == "" {
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require %s: local path requires a source file context", name)
	}

	target := name
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDir, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require %s: %v", name, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require %s: directory %q does not exist", name, abs)
	}
	if !info.IsDir() {
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require %s: path %q is a file, directory required", name, abs)
	}

	// A repo root may contain only a nested adapter module. Recognize it
	// before the loose-package fallback so its own go.mod and module identity
	// remain authoritative even when the parent directory has no go.mod.
	adapterDir := filepath.Join(abs, "nexssflow")
	adapterGoMod := filepath.Join(adapterDir, "go.mod")
	if HasGoFiles(adapterDir) {
		if adapterInfo, statErr := os.Stat(adapterGoMod); statErr == nil && !adapterInfo.IsDir() {
			modulePath, readErr := ReadModuleLine(adapterGoMod)
			if readErr != nil {
				return Requirement{}, core.SourceError(
					core.Position{File: file, Line: line},
					"@require %s: read nested adapter module: %v", name, readErr)
			}
			return Requirement{
				Import:      modulePath,
				PackagePath: modulePath,
				LocalPath:   abs,
				ModuleRoot:  adapterDir,
				ModulePath:  modulePath,
				Options:     opts,
			}, nil
		}
	}

	moduleRoot, modulePath, err := findModuleRoot(abs)
	if err != nil {
		if HasGoFiles(abs) || HasGoFiles(filepath.Join(abs, "nexssflow")) {
			looseID := synthesizeLooseID(abs)
			return Requirement{
				Import:    "nflow-harness/loose/" + looseID,
				LocalPath: abs,
				IsLoose:   true,
				LooseID:   looseID,
				Options:   opts,
			}, nil
		}

		wsRoot := findWorkspaceBoundary(abs)
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require %s: no go.mod found above %q and directory contains no .go files\n\n"+
				"  💡 Quick fix:\n"+
				"     1. Place your extension .go files directly inside %q\n"+
				"     2. Or run `go mod init <name>` in %q to establish a module root",
			name, abs, abs, wsRoot)
	}

	rel, err := filepath.Rel(moduleRoot, abs)
	if err != nil {
		return Requirement{}, core.SourceError(
			core.Position{File: file, Line: line},
			"@require %s: %v", name, err)
	}

	importPath := modulePath
	if rel != "." && rel != "" {
		importPath = modulePath + "/" + filepath.ToSlash(rel)
	}

	return Requirement{
		Import:     importPath,
		LocalPath:  abs,
		ModuleRoot: moduleRoot,
		ModulePath: modulePath,
		Options:    opts,
	}, nil
}

func findWorkspaceBoundary(start string) string {
	curr := start
	if info, err := os.Stat(curr); err == nil && !info.IsDir() {
		curr = filepath.Dir(curr)
	}
	for {
		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return curr
		}
		if _, err := os.Stat(filepath.Join(curr, "go.work")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return start
		}
		curr = parent
	}
}

func HasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

func synthesizeLooseID(dir string) string {
	base := strings.ToLower(filepath.Base(dir))
	var clean strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			clean.WriteRune(r)
		case r == '-', r == '_':
			clean.WriteRune(r)
		default:
			clean.WriteByte('_')
		}
	}
	if clean.Len() == 0 {
		clean.WriteString("pkg")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return fmt.Sprintf("%s_%s", clean.String(), hex.EncodeToString(sum[:4]))
}

func findModuleRoot(dir string) (root, modulePath string, err error) {
	curr := dir
	for {
		if isSystemTempDir(curr) {
			return "", "", fmt.Errorf("no go.mod found within workspace (reached temp root %s)", curr)
		}

		goModPath := filepath.Join(curr, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			modulePath, err := ReadModuleLine(goModPath)
			if err != nil {
				return "", "", err
			}
			return curr, modulePath, nil
		}

		if _, err := os.Stat(filepath.Join(curr, ".git")); err == nil {
			return "", "", fmt.Errorf("no go.mod found within repository %s", curr)
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			return "", "", fmt.Errorf("no go.mod found above %s", dir)
		}
		curr = parent
	}
}

func isSystemTempDir(dir string) bool {
	cleanDir := filepath.Clean(dir)
	temp := filepath.Clean(os.TempDir())
	if strings.EqualFold(cleanDir, temp) {
		return true
	}
	if env := os.Getenv("TEMP"); env != "" && strings.EqualFold(cleanDir, filepath.Clean(env)) {
		return true
	}
	if env := os.Getenv("TMP"); env != "" && strings.EqualFold(cleanDir, filepath.Clean(env)) {
		return true
	}
	return false
}

func ReadModuleLine(goModPath string) (string, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s has no module directive", goModPath)
}

func Resolve(plan Plan, _ string) ([]ResolvedModule, error) {
	out := make([]ResolvedModule, 0, len(plan.Modules))
	for i := range plan.Modules {
		req := &plan.Modules[i]
		if req.LocalPath != "" {
			out = append(out, ResolvedModule{
				Requirement: *req,
				Dir:         req.LocalPath,
				GoMod:       filepath.Join(req.ModuleRoot, "go.mod"),
			})
			continue
		}
		out = append(out, ResolvedModule{Requirement: *req})
	}
	return out, nil
}
