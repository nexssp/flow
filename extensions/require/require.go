package require

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/module"

	compiler "github.com/nexssp/flow/compiler"
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

// FromMeta returns requirements in declaration/include order and preserves
// metadata and conflict errors for callers that plan or build from metadata.
func FromMeta(meta map[string]any) ([]Requirement, error) {
	return SourceRequirements(meta)
}

type sourceRequirements struct {
	values  []Requirement
	seen    map[string]requirementOrigin
	aliases map[string]requirementOrigin
}

type requirementOrigin struct {
	target        string
	position      compiler.Position
	positionKnown bool
	options       map[string]string
}

// SourceRequirements discovers dependencies through parsed source modules.
// Identical duplicates are idempotent; conflicting options and alias collisions
// are reported in source order with source positions.
func SourceRequirements(meta map[string]any) ([]Requirement, error) {
	collected := &sourceRequirements{seen: make(map[string]requirementOrigin), aliases: make(map[string]requirementOrigin)}
	if err := collected.visit(meta); err != nil {
		return nil, err
	}
	return collected.values, nil
}

func (collected *sourceRequirements) visit(meta map[string]any) error {
	if meta == nil {
		return nil
	}
	local, imported, err := sourceRequirementsFromMeta(meta)
	if err != nil {
		return err
	}
	next := 0
	for i := range local {
		candidate := &local[i]
		if candidate.Import == "" {
			return fmt.Errorf("require metadata entry %d has an empty import target", i+1)
		}
		key := requirementKey(candidate)
		position, positionKnown := compiler.DeclarationPosition(meta, "require", key)
		if !positionKnown && len(imported) > 0 {
			return fmt.Errorf("require metadata for %q is missing a source position; include ordering cannot be determined", candidate.Import)
		}
		next, err = collected.visitImportsBefore(imported, next, position.Line)
		if err != nil {
			return err
		}
		if err := collected.add(candidate, position, positionKnown); err != nil {
			return err
		}
	}
	return collected.visitImports(imported, next)
}

func sourceRequirementsFromMeta(meta map[string]any) ([]Requirement, []compiler.SourceImport, error) {
	var local []Requirement
	if raw, exists := meta["require"]; exists {
		var ok bool
		local, ok = raw.([]Requirement)
		if !ok {
			return nil, nil, fmt.Errorf("require metadata has type %T, want []require.Requirement", raw)
		}
	}
	if raw, exists := meta[sourceDeclarationsKey]; exists {
		if _, ok := raw.([]SourceDeclaration); !ok {
			return nil, nil, fmt.Errorf("require declaration metadata has type %T, want []require.SourceDeclaration", raw)
		}
	}
	imported := compiler.SourceImports(meta)
	for i, child := range imported {
		if child.Source == nil {
			return nil, nil, fmt.Errorf("require metadata import %d has no prepared source", i+1)
		}
		if child.Position.Line < 1 {
			return nil, nil, fmt.Errorf("require metadata import %d has invalid source position %s", i+1, child.Position)
		}
	}
	return local, imported, nil
}

func (collected *sourceRequirements) visitImportsBefore(imported []compiler.SourceImport, next, line int) (int, error) {
	for next < len(imported) && imported[next].Position.Line < line {
		if err := collected.visit(imported[next].Source.Metadata()); err != nil {
			return next, err
		}
		next++
	}
	return next, nil
}

func (collected *sourceRequirements) visitImports(imported []compiler.SourceImport, start int) error {
	for i := start; i < len(imported); i++ {
		if err := collected.visit(imported[i].Source.Metadata()); err != nil {
			return err
		}
	}
	return nil
}

func (collected *sourceRequirements) add(candidate *Requirement, position compiler.Position, positionKnown bool) error {
	key := requirementKey(candidate)
	if previous, exists := collected.seen[key]; exists {
		if !maps.Equal(candidate.Options, previous.options) {
			if !positionKnown || !previous.positionKnown {
				return fmt.Errorf("@require: duplicate declaration for %q has conflicting options but source position metadata is missing", candidate.Import)
			}
			return compiler.SourceError(
				position,
				"@require: duplicate declaration for %q (version %q, alias %q) has conflicting options; first declared at %s",
				candidate.Import, candidate.Version, candidate.Alias, previous.position,
			)
		}
		return nil
	}
	origin := requirementOrigin{
		target: candidate.Import, position: position, positionKnown: positionKnown,
		options: maps.Clone(candidate.Options),
	}
	if candidate.Alias != "" {
		if prior, exists := collected.aliases[candidate.Alias]; exists {
			if !positionKnown || !prior.positionKnown {
				return fmt.Errorf("@require: namespace qualifier %q is reused but source position metadata is missing", candidate.Alias)
			}
			return compiler.SourceError(position, "@require: namespace qualifier %q is already used for module %q; first declared at %s", candidate.Alias, prior.target, prior.position)
		}
		collected.aliases[candidate.Alias] = origin
	}
	collected.seen[key] = origin
	collected.values = append(collected.values, *candidate)
	return nil
}

func requirementKey(requirement *Requirement) string {
	return requirement.Import + "\x00" + requirement.Version + "\x00" + requirement.Alias
}

type Plan struct {
	Modules []Requirement
}

type ResolvedModule struct {
	Requirement Requirement
	Dir         string
	GoMod       string
}

type requirementToken struct {
	raw        string
	start, end int
}

// SourceDeclaration identifies one direct @require declaration in the source
// file that produced metadata. Selector offsets are byte offsets into the
// original, complete directive line and include any selector quote marks.
type SourceDeclaration struct {
	// SourceLine is the executed header, used to reject edits after substitutions.
	SourceLine    string
	Requirement   Requirement
	Line          int
	SelectorStart int
	SelectorEnd   int
}

const sourceDeclarationsKey = "require.source.declarations"

// DirectDeclarations returns only @require directives executed in this
// metadata scope. Included source files retain their own declaration lists.
func DirectDeclarations(meta map[string]any) []SourceDeclaration {
	declarations, _ := meta[sourceDeclarationsKey].([]SourceDeclaration)
	return append([]SourceDeclaration(nil), declarations...)
}

func Parse(spec, baseDir string, opts map[string]string, file string, line int) (Requirement, error) {
	requirement, _, _, err := parseRequirement(spec, baseDir, opts, file, line)
	return requirement, err
}

// parseRequirement is the single grammar implementation for both public
// parsing and source declaration capture. The returned selector bounds are
// relative to spec; when no selector occurs, both are the end of the import
// token, ready for a source updater to insert one.
func parseRequirement(spec, baseDir string, opts map[string]string, file string, line int) (requirement Requirement, selectorStart, selectorEnd int, err error) {
	cleanSpec := stripRequirementComment(spec)
	parts := tokenizeRequirement(cleanSpec)
	if len(parts) == 0 {
		return Requirement{}, 0, 0, compiler.SourceError(
			compiler.Position{File: file, Line: line},
			"@require: missing module target")
	}

	importToken := parts[0]
	importPath := strings.Trim(importToken.raw, `"'`)
	parts = parts[1:]

	version := ""
	hasVersion := false
	alias := ""
	selectorStart, selectorEnd = importToken.end, importToken.end
	for len(parts) > 0 {
		if parts[0].raw == "as" {
			if alias != "" {
				return Requirement{}, 0, 0, compiler.SourceError(
					compiler.Position{File: file, Line: line},
					"@require: only one local namespace qualifier is allowed")
			}
			if len(parts) < 2 {
				return Requirement{}, 0, 0, compiler.SourceError(
					compiler.Position{File: file, Line: line},
					"@require: missing local namespace qualifier after `as`")
			}
			alias = strings.Trim(parts[1].raw, `"'`)
			if !validNamespaceQualifier(alias) {
				return Requirement{}, 0, 0, compiler.SourceError(
					compiler.Position{File: file, Line: line},
					"@require: invalid local namespace qualifier %q", alias)
			}
			parts = parts[2:]
			continue
		}
		if hasVersion {
			return Requirement{}, 0, 0, compiler.SourceError(
				compiler.Position{File: file, Line: line},
				"@require: extraneous tokens after declaration: %v", tokenValues(parts))
		}
		var selectorErr error
		version, selectorErr = parseRemoteSelector(parts[0].raw)
		if selectorErr != nil {
			return Requirement{}, 0, 0, compiler.SourceError(
				compiler.Position{File: file, Line: line},
				"@require: invalid selector %q: %v", parts[0].raw, selectorErr)
		}
		selectorStart, selectorEnd = parts[0].start, parts[0].end
		hasVersion = true
		parts = parts[1:]
	}

	if isLocal(importPath) {
		if version != "" {
			return Requirement{}, 0, 0, compiler.SourceError(
				compiler.Position{File: file, Line: line},
				"@require: local path must not carry a version: %q", importPath)
		}
		req, err := resolveLocal(importPath, baseDir, opts, file, line)
		if err != nil {
			return Requirement{}, 0, 0, err
		}
		req.Alias = alias
		return req, selectorStart, selectorEnd, nil
	}

	return Requirement{
		Import:  importPath,
		Alias:   alias,
		Version: version,
		Options: opts,
	}, selectorStart, selectorEnd, nil
}

func tokenizeRequirement(spec string) []requirementToken {
	var tokens []requirementToken
	for i := 0; i < len(spec); {
		for i < len(spec) {
			r, size := utf8.DecodeRuneInString(spec[i:])
			if !unicode.IsSpace(r) {
				break
			}
			i += size
		}
		if i == len(spec) {
			break
		}
		start := i
		var quote byte
		for i < len(spec) {
			c := spec[i]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				i++
				continue
			}
			if c == '"' || c == '\'' {
				quote = c
				i++
				continue
			}
			r, size := utf8.DecodeRuneInString(spec[i:])
			if unicode.IsSpace(r) {
				break
			}
			i += size
		}
		tokens = append(tokens, requirementToken{raw: spec[start:i], start: start, end: i})
	}
	return tokens
}

func tokenValues(tokens []requirementToken) []string {
	values := make([]string, len(tokens))
	for i := range tokens {
		values[i] = tokens[i].raw
	}
	return values
}

// stripRequirementComment recognizes # and // only outside quotes and at a
// token boundary. This keeps URLs and quoted paths/selectors intact.
func stripRequirementComment(spec string) string {
	var quote byte
	for i := range len(spec) {
		c := spec[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if (c == '#' || c == '/' && i+1 < len(spec) && spec[i+1] == '/') &&
			(i == 0 || isSpaceBefore(spec, i)) {
			return spec[:i]
		}
	}
	return spec
}

func isSpaceBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsSpace(r)
}

// parseRemoteSelector validates the selector passed to Go's module query
// syntax. Selectors are broader than semantic versions: they may be tags,
// branch names (including slash-separated names), commit hashes, or keywords
// such as "latest". CheckFilePath provides Go's conservative path-character
// validation; the extra checks avoid ambiguous or unsafe @require tokens.
func parseRemoteSelector(raw string) (string, error) {
	selector := raw
	if strings.HasPrefix(raw, `"`) || strings.HasPrefix(raw, `'`) {
		quote := raw[0]
		if len(raw) < 2 || raw[len(raw)-1] != quote {
			return "", errors.New("unmatched quote")
		}
		selector = raw[1 : len(raw)-1]
	} else if strings.ContainsAny(raw, `"'`) {
		return "", errors.New("quotes must surround the entire selector")
	}
	if selector == "" {
		return "", errors.New("selector must not be empty")
	}
	if strings.HasPrefix(selector, "-") {
		return "", errors.New("selector must not begin with '-'")
	}
	for _, r := range selector {
		if r == '@' {
			return "", errors.New("selector must not contain '@'")
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", errors.New("selector must not contain whitespace or control characters")
		}
	}
	if err := module.CheckFilePath(selector); err != nil {
		return "", err
	}
	return selector, nil
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
		return Requirement{}, compiler.SourceError(
			compiler.Position{File: file, Line: line},
			"@require %s: local path requires a source file context", name)
	}

	target := name
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDir, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return Requirement{}, compiler.SourceError(
			compiler.Position{File: file, Line: line},
			"@require %s: %v", name, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Requirement{}, compiler.SourceError(
			compiler.Position{File: file, Line: line},
			"@require %s: directory %q does not exist", name, abs)
	}
	if !info.IsDir() {
		return Requirement{}, compiler.SourceError(
			compiler.Position{File: file, Line: line},
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
				return Requirement{}, compiler.SourceError(
					compiler.Position{File: file, Line: line},
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
		return Requirement{}, compiler.SourceError(
			compiler.Position{File: file, Line: line},
			"@require %s: no go.mod found above %q and directory contains no .go files\n\n"+
				"  💡 Quick fix:\n"+
				"     1. Place your extension .go files directly inside %q\n"+
				"     2. Or run `go mod init <name>` in %q to establish a module root",
			name, abs, abs, wsRoot)
	}

	rel, err := filepath.Rel(moduleRoot, abs)
	if err != nil {
		return Requirement{}, compiler.SourceError(
			compiler.Position{File: file, Line: line},
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
