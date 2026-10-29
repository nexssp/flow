package compiler

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/kernel/xfile"
)

type PreprocessContributions struct {
	Primaries   []PrimaryExtension
	CompileOpts []CompileOption
}

type SelfTestFeature struct {
	Name  string
	DSL   string
	Files map[string]string
	// SyntaxOnly marks fixtures that must be compiled but never executed by
	// the ordinary self-test path. Fixtures opt in with `# @syntax-only`.
	SyntaxOnly         bool
	Skip               string
	Source             string
	ExpectCompileError string
}

type SelfTestSection struct {
	Name     string
	Features []SelfTestFeature
}

// ShutdownFunc releases a resource explicitly owned by a bundle.
// Implementations must honor ctx so host shutdown remains bounded.
type ShutdownFunc func(context.Context) error

// ScopeInheritFunc copies extension-owned declarations from a parent
// compilation scope into a child scope before the child's directives run.
// The parent metadata is read-only; the callback should copy values it keeps.
type ScopeInheritFunc func(parentMeta, childMeta map[string]any) error

// FileResolverBinding routes references from one provider to its resolver.
type FileResolverBinding struct {
	Provider xfile.Provider
	Resolver xfile.Resolver
}

// BuildExtension describes the source-level dependencies of a bundle for
// generated Go builds. It is deliberately a token-level linking contract,
// not an AST-level guarantee that every selected action executes.
type BuildExtension struct {
	Always    bool
	Modifiers []string
	Requires  []string
	Replaces  []string
	Sources   func(meta map[string]any) []string
}

// Bundle is one extension distribution.
type Bundle struct {
	ID          string
	Alias       string // Local @require qualifier replacing library namespaces for this compilation.
	Libraries   []action.Library
	Directives  []Directive
	Modifiers   []Modifier
	Operators   []Operator
	Primaries   []PrimaryExtension
	Materialize Materializer
	// MaterializePhase defaults to MaterializeDeclarations; see materializer-lifecycle.md.
	MaterializePhase MaterializePhase
	OnPreprocess     func(meta map[string]any) PreprocessContributions
	// InheritScope copies extension-owned compile metadata into a child scope
	// before child directives run. Implement it when child preprocessing depends
	// on parent declarations; propagation is opt-in per bundle, and this callback
	// must merge into child metadata rather than replacing other extensions' data.
	// Treat parentMeta as read-only and copy values retained by the child.
	InheritScope  ScopeInheritFunc
	AtomAdvise    func(atom *Atom, builder *action.Builder[any, any]) error
	WrapPipeline  func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error)
	Hooks         []action.AnyHook
	FileResolvers []FileResolverBinding

	ArgSchemas map[string][]ArgFieldSpec

	AcceptedOptions []string
	Symbols         []SymbolNamespace

	SelfTest func() []SelfTestSection
	Fixtures fs.FS
	// SourceModules contains compile-time .nflow sources addressable through
	// @include. Only paths listed in ExportedModules are visible to consumers.
	SourceModules   fs.FS
	ExportedModules []string

	// Shutdowns are invoked by the runner host after the outer invocation
	// completes. Compile and Execute do not close bundle resources.
	Shutdowns []ShutdownFunc

	// Build contributes optional source-level build planning metadata.
	Build BuildExtension
}

// BundleWithFileResolver returns a copy of bundle with one additional typed
// file-resolver binding. runner.BuildConfig composes bindings across bundles.
func BundleWithFileResolver(bundle Bundle, provider xfile.Provider, resolver xfile.Resolver) Bundle {
	bundle.FileResolvers = slices.Clone(bundle.FileResolvers)
	bundle.FileResolvers = append(bundle.FileResolvers, FileResolverBinding{Provider: provider, Resolver: resolver})
	return bundle
}

func ValidateBundle(b Bundle) error {
	if strings.TrimSpace(b.ID) == "" {
		return errors.New("bundle: ID is required")
	}
	switch b.MaterializePhase {
	case MaterializeDeclarations, MaterializeCompositions:
	default:
		return xerr.Validation(fmt.Sprintf("bundle %q: invalid materialize phase %d", b.ID, b.MaterializePhase))
	}
	if b.Alias != "" && !validNamespaceQualifier(b.Alias) {
		return fmt.Errorf("bundle %q: invalid @require namespace qualifier %q", b.ID, b.Alias)
	}
	if err := validateBundleSourceModules(b); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(b.Libraries))
	for i := range b.Libraries {
		lib := b.Libraries[i]
		name := strings.TrimSpace(lib.Name)
		if name == "" {
			return fmt.Errorf("bundle %q: library %d has empty name", b.ID, i)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("bundle %q: duplicate library %q", b.ID, name)
		}
		seen[name] = struct{}{}
	}
	for i, binding := range b.FileResolvers {
		provider := string(binding.Provider)
		if provider == "" || strings.TrimSpace(provider) != provider {
			return fmt.Errorf("bundle %q: file resolver %d has an invalid provider", b.ID, i)
		}
		if binding.Resolver == nil {
			return fmt.Errorf("bundle %q: file resolver %q is nil", b.ID, provider)
		}
	}

	seenNamespaces := make(map[string]struct{}, len(b.Symbols))
	for i := range b.Symbols {
		ns := b.Symbols[i]
		if strings.TrimSpace(ns.Name) == "" {
			return fmt.Errorf("bundle %q: symbol namespace %d has empty name", b.ID, i)
		}
		if len(ns.Symbols) == 0 {
			return fmt.Errorf("bundle %q: symbol namespace %q has no symbols", b.ID, ns.Name)
		}
		if _, dup := seenNamespaces[ns.Name]; dup {
			return fmt.Errorf("bundle %q: duplicate symbol namespace %q", b.ID, ns.Name)
		}
		seenNamespaces[ns.Name] = struct{}{}
	}
	return nil
}

type BundleFactory func(opts map[string]string) Bundle

var (
	bundleMu        sync.RWMutex
	bundleFactories = map[string]BundleFactory{}
)

// Register registers a bundle factory under the given ID.
func Register(id string, factory BundleFactory) {
	if factory == nil {
		panic("core: Register called with nil factory")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		panic("core: Register called with empty ID")
	}
	bundleMu.Lock()
	defer bundleMu.Unlock()
	if _, exists := bundleFactories[id]; exists {
		panic(fmt.Sprintf("core: duplicate bundle ID %q", id))
	}
	bundleFactories[id] = factory
}

// RegisterBundle is a compatibility alias for Register.
func RegisterBundle(id string, factory BundleFactory) {
	Register(id, factory)
}

// Lookup returns the factory registered for the exact ID.
func Lookup(id string) (BundleFactory, bool) {
	bundleMu.RLock()
	defer bundleMu.RUnlock()
	f, ok := bundleFactories[id]
	return f, ok
}

// LookupBundleForModule provides backward compatibility for unit tests.
func LookupBundleForModule(target string) (BundleFactory, bool) {
	if f, ok := Lookup(target); ok {
		return f, true
	}
	clean := strings.ReplaceAll(target, `\`, "/")
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		return nil, false
	}
	base := path.Base(clean)
	if base == "nexssflow" {
		parent := path.Base(path.Dir(clean))
		if parent != "." && parent != "/" && parent != "" {
			return Lookup(parent)
		}
	}
	return Lookup(base)
}

func (b Bundle) AllSelfTests() []SelfTestSection {
	sections := []SelfTestSection{}
	if b.SelfTest != nil {
		sections = append(sections, b.SelfTest()...)
	}

	fixtureFeatures := discoverFixtures(b.ID, b.Fixtures)
	if len(fixtureFeatures) == 0 {
		return sections
	}

	for i := range sections {
		if sections[i].Name == b.ID {
			sections[i].Features = append(sections[i].Features, fixtureFeatures...)
			return sections
		}
	}
	return append(sections, SelfTestSection{Name: b.ID, Features: fixtureFeatures})
}

func discoverFixtures(bundleID string, fsys fs.FS) []SelfTestFeature {
	if fsys == nil {
		return nil
	}

	var candidates []string
	walkErr := fs.WalkDir(fsys, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err // let WalkDir report it
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".nflow") {
			return nil
		}
		candidates = append(candidates, p)
		return nil
	})
	if walkErr != nil {
		// fixtures are best-effort; record and continue
		return nil
	}

	sort.Strings(candidates)
	paths := make([]string, 0, len(candidates))
	for _, p := range candidates {
		if isFixtureCompanionPath(fsys, p) || isFlatFixtureCompanionPath(p, candidates) {
			continue
		}
		paths = append(paths, p)
	}

	features := make([]SelfTestFeature, 0, len(paths))
	for _, p := range paths {
		source := path.Join(bundleID, p)
		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			features = append(features, SelfTestFeature{
				Name:   path.Base(p),
				Skip:   "fixture read failed: " + err.Error(),
				Source: source,
			})
			continue
		}
		features = append(features, SelfTestFeature{
			Name:       fixtureName(p, string(content)),
			DSL:        string(content),
			Files:      readFixtureFiles(fsys, p),
			SyntaxOnly: fixtureSyntaxOnly(string(content)),
			Source:     source,
		})
	}
	return features
}

func fixtureSyntaxOnly(source string) bool {
	for line := range strings.SplitSeq(source, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "# @syntax-only") {
			return true
		}
	}
	return false
}

// isFixtureCompanionPath reports whether p is nested beneath the companion
// directory of another fixture. Included child flows belong to their parent
// example and must not also run as standalone self-tests.
func isFixtureCompanionPath(fsys fs.FS, p string) bool {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		info, err := fs.Stat(fsys, dir+".nflow")
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// isFlatFixtureCompanionPath reports whether p is a flattened sidecar flow,
// encoded as <fixture-stem>--<relative-path>.nflow beside its parent fixture.
func isFlatFixtureCompanionPath(p string, candidates []string) bool {
	name := path.Base(p)
	for _, candidate := range candidates {
		if candidate == p || path.Dir(candidate) != path.Dir(p) {
			continue
		}
		stem := strings.TrimSuffix(path.Base(candidate), ".nflow")
		if strings.HasPrefix(name, stem+"--") {
			return true
		}
	}
	return false
}

// readFixtureFiles loads companion files from either the legacy per-fixture
// directory or flat files named <fixture-stem>--<relative-path>. For example,
// read.nflow can use read--input.json, which is exposed to the fixture as
// input.json; read--schemas--input.yml is exposed as schemas/input.yml.
func readFixtureFiles(fsys fs.FS, fixturePath string) map[string]string {
	files := make(map[string]string)
	legacyDir := strings.TrimSuffix(fixturePath, ".nflow")
	if info, err := fs.Stat(fsys, legacyDir); err == nil && info.IsDir() {
		walkErr := fs.WalkDir(fsys, legacyDir, func(p string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel := strings.TrimPrefix(p, legacyDir+"/")
			if !fs.ValidPath(rel) {
				return fs.ErrInvalid
			}
			content, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			files[rel] = string(content)
			return nil
		})
		if walkErr != nil {
			return nil
		}
	}

	parent := path.Dir(fixturePath)
	prefix := path.Base(strings.TrimSuffix(fixturePath, ".nflow")) + "--"
	entries, err := fs.ReadDir(fsys, parent)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
				continue
			}
			encoded := strings.TrimPrefix(entry.Name(), prefix)
			rel := strings.ReplaceAll(encoded, "--", "/")
			if !fs.ValidPath(rel) {
				continue
			}
			content, err := fs.ReadFile(fsys, path.Join(parent, entry.Name()))
			if err != nil {
				return nil
			}
			files[rel] = string(content)
		}
	}
	if len(files) == 0 {
		return nil
	}
	return files
}

func fixtureName(fixturePath, source string) string {
	for line := range strings.SplitSeq(source, "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "@description")
		if !ok {
			continue
		}
		if name := strings.Trim(strings.TrimSpace(rest), `"'`); name != "" {
			return name
		}
	}
	return strings.TrimSuffix(path.Base(fixturePath), ".nflow")
}
