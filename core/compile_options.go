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
	lineMods     []func(line int) []string
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

// WithConfigMap carries @config resolution context: the merged
// key→value map plus the raw CLI args for @flag. lookups.
func WithConfigMap(cfg map[string]string, cliArgs []string) CompileOption {
	return func(c *compileConfig) {
		c.config = cfg
		c.cliArgs = cliArgs
	}
}

// WithLineModifiers appends a line-indexed modifier lookup. Multiple
// options accumulate; later ones override earlier ones on the same
// modifier name. Extensions compose lookups this way — a pipeline's
// inherited policy is registered before nested @scope spans.
func WithLineModifiers(fn func(line int) []string) CompileOption {
	return func(c *compileConfig) {
		if fn != nil {
			c.lineMods = append(c.lineMods, fn)
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
// modifiers pass through for ApplyAll to reject.
//
// Used by nflow lint to parse a source with the same inherited
// modifiers the runtime compiler would apply. Extensions publish their
// lookups through Bundle.OnPreprocess as CompileOptions; this function
// is the only way to extract them outside CompileAction.
func LineModifiersFromOptions(mt *ModifierTable, opts []CompileOption) []func(int) []string {
	return filterLineMods(mt, applyCompileOptions(opts).lineMods)
}

func filterLineMods(mt *ModifierTable, fns []func(int) []string) []func(int) []string {
	if len(fns) == 0 {
		return nil
	}
	out := make([]func(int) []string, len(fns))
	for i, fn := range fns {
		out[i] = filterInheritable(fn, mt)
	}
	return out
}
