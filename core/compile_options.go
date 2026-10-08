package core

import (
	"context"
	"maps"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
)

type AtomAdviseFunc func(atom *Atom, builder *action.Builder[any, any]) error

type PipelineWrapFunc func(meta map[string]any, inner action.AnyAction) (action.AnyAction, error)

// OnPreprocessFunc is the compile-time hook fired between Preprocess
// and Parse. BundleConfig wires it from Bundle.OnPreprocess.
type OnPreprocessFunc func(meta map[string]any) PreprocessContributions

type compileConfig struct {
	advisors     []AtomAdviseFunc
	wrappers     []PipelineWrapFunc
	onPreprocess []OnPreprocessFunc
	config       map[string]string
	cliArgs      []string
	argSchemas   map[string][]ArgFieldSpec
	lineMods     []LineLookup
}

type CompileOption func(*compileConfig)

func WithArgSchemas(schemas map[string][]ArgFieldSpec) CompileOption {
	return func(c *compileConfig) {
		if len(schemas) == 0 {
			return
		}
		if c.argSchemas == nil {
			c.argSchemas = make(map[string][]ArgFieldSpec, len(schemas))
		}
		maps.Copy(c.argSchemas, schemas)
	}
}

func WithAtomAdvisors(fns ...AtomAdviseFunc) CompileOption {
	return func(c *compileConfig) {
		c.advisors = append(c.advisors, fns...)
	}
}

func WithPipelineWrappers(fns ...PipelineWrapFunc) CompileOption {
	return func(c *compileConfig) {
		c.wrappers = append(c.wrappers, fns...)
	}
}

func WithOnPreprocess(fns ...OnPreprocessFunc) CompileOption {
	return func(c *compileConfig) {
		c.onPreprocess = append(c.onPreprocess, fns...)
	}
}

// WithConfigMap carries the @config key→value map into the compile
// context. It is called by the config bundle's OnPreprocess hook after
// preprocessing has collected the source's @config directives. A nil
// map leaves any existing config untouched so an unrelated option (e.g.
// WithCLIArgs from the CLI) is not silently wiped by a later call.
func WithConfigMap(cfg map[string]string) CompileOption {
	return func(c *compileConfig) {
		if cfg != nil {
			c.config = cfg
		}
	}
}

// WithCLIArgs carries the raw command-line flags into the compile
// context so @flag.X references can be resolved and validated at
// compile time. A nil slice disables @flag validation, which is the
// right behavior for embedded runs and pure-compile tests that do not
// model a CLI invocation; an empty non-nil slice enables validation
// with zero known flags, so any @flag reference in the source is
// rejected.
func WithCLIArgs(args []string) CompileOption {
	return func(c *compileConfig) {
		if args != nil {
			c.cliArgs = args
		}
	}
}

// WithLineModifiers appends line-indexed modifier lookups. Multiple
// options accumulate; later lookups override earlier ones on the same
// modifier name. Extensions compose lookups this way — a pipeline's
// inherited policy is registered before nested @scope spans.
func WithLineModifiers(lookups ...LineLookup) CompileOption {
	return func(c *compileConfig) {
		for _, lk := range lookups {
			if lk.Fn != nil {
				c.lineMods = append(c.lineMods, lk)
			}
		}
	}
}

// BundleConfig collects every extension point from a set of bundles
// into a single set of CompileOptions. Called once at startup.
func BundleConfig(bundles ...Bundle) []CompileOption {
	var (
		advisors []AtomAdviseFunc
		wrappers []PipelineWrapFunc
		onPre    []OnPreprocessFunc
		schemas  = map[string][]ArgFieldSpec{}
	)
	for i := range bundles {
		b := &bundles[i]
		if b.AtomAdvise != nil {
			advisors = append(advisors, b.AtomAdvise)
		}
		if b.WrapPipeline != nil {
			wrappers = append(wrappers, b.WrapPipeline)
		}
		if b.OnPreprocess != nil {
			onPre = append(onPre, b.OnPreprocess)
		}
		maps.Copy(schemas, b.ArgSchemas)
	}

	opts := make([]CompileOption, 0, 4)
	if len(advisors) > 0 {
		opts = append(opts, WithAtomAdvisors(advisors...))
	}
	if len(wrappers) > 0 {
		opts = append(opts, WithPipelineWrappers(wrappers...))
	}
	if len(onPre) > 0 {
		opts = append(opts, WithOnPreprocess(onPre...))
	}
	if len(schemas) > 0 {
		opts = append(opts, WithArgSchemas(schemas))
	}
	return opts
}

// ── Compile-time config plumbing ────────────────────────────────────

type compileConfigValue struct {
	config  map[string]string
	cliArgs []string
}

var compileConfigKey = xctx.NewKey[compileConfigValue]("flow.compile.config")

func WithCompileConfig(ctx context.Context, cfg map[string]string, cliArgs []string) context.Context {
	if len(cfg) == 0 && len(cliArgs) == 0 {
		return ctx
	}
	return compileConfigKey.With(ctx, compileConfigValue{config: cfg, cliArgs: cliArgs})
}

func compileConfigFromCtx(ctx context.Context) (config map[string]string, cliArgs []string) {
	v, _ := compileConfigKey.From(ctx)
	return v.config, v.cliArgs
}

// LineModifiersFromOptions returns the line-indexed modifier lookups
// that the given CompileOptions would install, each filtered through
// mt so non-inheritable metadata modifiers are dropped and unknown
// modifiers pass through for ApplyAll to reject. Sources are preserved.
//
// Used by diagnostics such as nflow explain that need to render inherited
// modifiers without compiling a program. Extensions publish their lookups
// through Bundle.OnPreprocess as CompileOptions.
func LineModifiersFromOptions(mt *ModifierTable, opts []CompileOption) []LineLookup {
	return filterLineMods(mt, applyCompileOptions(opts).lineMods)
}

func filterLineMods(mt *ModifierTable, lookups []LineLookup) []LineLookup {
	if len(lookups) == 0 {
		return nil
	}
	out := make([]LineLookup, len(lookups))
	for i, lk := range lookups {
		out[i] = filterInheritable(lk, mt)
	}
	return out
}
