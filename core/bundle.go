package core

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/nexssp/kernel/action"
)

type PreprocessContributions struct {
	Primaries   []PrimaryExtension
	CompileOpts []CompileOption
}

type SelfTestFeature struct {
	Name   string
	DSL    string
	Files  map[string]string
	Skip   string
	Source string
}

type SelfTestSection struct {
	Name     string
	Features []SelfTestFeature
}

// Bundle is one extension distribution.
type Bundle struct {
	ID           string
	Alias        string // Local @require qualifier replacing library namespaces for this compilation.
	Libraries    []action.Library
	Directives   []Directive
	Modifiers    []Modifier
	Operators    []Operator
	Primaries    []PrimaryExtension
	Materialize  Materializer
	OnPreprocess func(meta map[string]any) PreprocessContributions
	AtomAdvise   func(atom *Atom, builder *action.Builder[any, any]) error
	WrapPipeline func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error)

	ArgSchemas map[string][]ArgFieldSpec

	AcceptedOptions []string

	SelfTest func() []SelfTestSection
	Fixtures fs.FS
}

func ValidateBundle(b Bundle) error {
	if strings.TrimSpace(b.ID) == "" {
		return errors.New("bundle: ID is required")
	}
	if b.Alias != "" && !validNamespaceQualifier(b.Alias) {
		return fmt.Errorf("bundle %q: invalid @require namespace qualifier %q", b.ID, b.Alias)
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

// RegisterBundle to alias do Register dla pełnej kompatybilności.
func RegisterBundle(id string, factory BundleFactory) {
	Register(id, factory)
}

// Lookup znajduje zarejestrowany bundle po dokładnym ID.
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

	var paths []string
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
		paths = append(paths, p)
		return nil
	})
	if walkErr != nil {
		// fixtures are best-effort; record and continue
		return nil
	}

	sort.Strings(paths)

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
			Name:   fixtureName(p, string(content)),
			DSL:    string(content),
			Source: source,
		})
	}
	return features
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
