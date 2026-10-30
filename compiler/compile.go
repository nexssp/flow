package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type CompileReq struct {
	Source             string
	Name               string
	Line               int
	InheritedPrimaries []PrimaryExtension
	InheritedMeta      map[string]any
	Prepared           *PreparedSource
}

// PreparedSource is the result of preprocessing one source. Its fields are
// intentionally opaque so callers can inspect metadata and pass the value to
// CompileAction without separating the source from its metadata.
type PreparedSource struct {
	source              string
	name                string
	clean               string
	meta                map[string]any
	contributions       PreprocessContributions
	actionIntrospection bool
}

// Metadata returns the directive metadata associated with this source.
func (p *PreparedSource) Metadata() map[string]any {
	if p == nil {
		return nil
	}
	return p.meta
}

// SourceIdentity returns the original source name and its SHA-256 digest.
// It lets callers bind durable state to a prepared source without exposing
// the source text stored inside this opaque value.
func (p *PreparedSource) SourceIdentity() (name, sha256Hex string) {
	if p == nil {
		return "", ""
	}
	sum := sha256.Sum256([]byte(p.source))
	return p.name, hex.EncodeToString(sum[:])
}

// CleanSource returns the preprocessed source that will be compiled.
func (p *PreparedSource) CleanSource() string {
	if p == nil {
		return ""
	}
	return p.clean
}

// ActionIntrospectionEnabled reports whether this source was prepared with
// Kernel composition capture requested.
func (p *PreparedSource) ActionIntrospectionEnabled() bool {
	return p != nil && p.actionIntrospection
}

// Contributions returns compile-time contributions already computed for this
// source. It does not invoke extension callbacks again.
func (p *PreparedSource) Contributions() PreprocessContributions {
	if p == nil {
		return PreprocessContributions{}
	}
	return PreprocessContributions{
		Primaries:   append([]PrimaryExtension(nil), p.contributions.Primaries...),
		CompileOpts: append([]CompileOption(nil), p.contributions.CompileOpts...),
	}
}

type CompileRes struct {
	Program     action.AnyAction
	Meta        map[string]any
	AST         Expr
	ActionGraph *ActionGraph `json:"action_graph,omitempty"`
}

func applyCompileOptions(opts []CompileOption) *compileConfig {
	cfg := &compileConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(cfg)
	}
	return cfg
}

func PreprocessContributionsFromMeta(meta map[string]any, opts ...CompileOption) PreprocessContributions {
	cfg := applyCompileOptions(opts)
	var out PreprocessContributions
	for _, fn := range cfg.onPreprocess {
		c := fn(meta)
		out.Primaries = append(out.Primaries, c.Primaries...)
		out.CompileOpts = append(out.CompileOpts, c.CompileOpts...)
	}
	return out
}

// PrepareSource preprocesses source and computes its extension contributions
// once. Use the returned value when metadata must be inspected before the same
// source is compiled, such as runner materialization.
func PrepareSource(ctx context.Context, dt *DirectiveTable, source, name string, opts ...CompileOption) (*PreparedSource, error) {
	cfg := applyCompileOptions(opts)
	return prepareSource(ctx, dt, source, name, cfg, nil)
}

// PreparedSourceFromPreprocessed binds an existing preprocessing result to its
// original source and computes compile-time contributions without parsing or
// preprocessing the source again.
func PreparedSourceFromPreprocessed(source, name, clean string, meta map[string]any, opts ...CompileOption) *PreparedSource {
	cfg := applyCompileOptions(opts)
	prepared := &PreparedSource{source: source, name: name, clean: clean, meta: meta, actionIntrospection: cfg.actionIntrospection}
	prepareContributions(prepared, cfg)
	return prepared
}

// PreprocessWithOptions preprocesses source with compile-time bundle sources
// and other options applied. It is useful to inspect metadata before compiling.
func PreprocessWithOptions(ctx context.Context, dt *DirectiveTable, source, name string, opts ...CompileOption) (clean string, meta map[string]any, err error) {
	cfg := applyCompileOptions(opts)
	ctx = applyConfigToCtx(ctx, cfg)
	return Preprocess(ctx, dt, source, name)
}

func prepareSource(ctx context.Context, dt *DirectiveTable, source, name string, cfg *compileConfig, inheritedMeta map[string]any) (*PreparedSource, error) {
	ctx = applyConfigToCtx(ctx, cfg)
	meta := make(map[string]any)
	if inheritedMeta != nil {
		for _, inherit := range cfg.scopeInheritors {
			if inherit == nil {
				continue
			}
			if err := inherit(inheritedMeta, meta); err != nil {
				return nil, fmt.Errorf("inherit compile scope: %w", err)
			}
		}
	}
	clean, meta, err := preprocessWithMeta(ctx, dt, source, name, meta)
	if err != nil {
		return nil, err
	}
	prepared := &PreparedSource{
		source: source, name: name, clean: clean, meta: meta,
		actionIntrospection: cfg.actionIntrospection,
	}
	prepareContributions(prepared, cfg)
	return prepared, nil
}

func prepareContributions(prepared *PreparedSource, cfg *compileConfig) {
	for _, imported := range SourceImports(prepared.meta) {
		imported.Source.actionIntrospection = cfg.actionIntrospection
		prepareContributions(imported.Source, cfg)
	}
	for _, fn := range cfg.onPreprocess {
		c := fn(prepared.meta)
		prepared.contributions.Primaries = append(prepared.contributions.Primaries, c.Primaries...)
		prepared.contributions.CompileOpts = append(prepared.contributions.CompileOpts, c.CompileOpts...)
	}
}

func CompileAction(
	resolver CapabilityResolver,
	dt *DirectiveTable,
	mt *ModifierTable,
	ot *OperatorTable,
	pt *PrimaryExtensionTable,
	opts ...CompileOption,
) *action.BuiltAction[CompileReq, CompileRes] {
	cfg := applyCompileOptions(opts)

	return action.New("compile", func(ctx context.Context, req CompileReq) (CompileRes, error) {
		return runCompile(ctx, cfg, opts, resolver, dt, mt, ot, pt, req)
	}).
		Description("Compile .nflow source into a runnable action").
		Tag("core", "compiler").
		Build()
}

func runCompile(
	ctx context.Context,
	cfg *compileConfig,
	opts []CompileOption,
	resolver CapabilityResolver,
	dt *DirectiveTable,
	mt *ModifierTable,
	ot *OperatorTable,
	pt *PrimaryExtensionTable,
	req CompileReq,
) (CompileRes, error) {
	if req.Prepared == nil && strings.TrimSpace(req.Source) == "" {
		return CompileRes{}, xerr.BadRequest("compile: source is empty")
	}

	ctx = applyConfigToCtx(ctx, cfg)

	prepared := req.Prepared
	if prepared == nil {
		var err error
		prepared, err = prepareSource(ctx, dt, req.Source, req.Name, cfg, req.InheritedMeta)
		if err != nil {
			return CompileRes{}, err
		}
	}
	// A declaration-only source (@const, @schema, @pipeline, @profile,
	// @include, ...) produces no pipeline tokens after preprocessing.
	// The old check tested for whitespace-only text, which misses the
	// case where the body is entirely `#` comments: comments are not
	// whitespace but the lexer skips them, so the parser sees only EOF.
	// Ask the lexer directly instead of guessing from the string.
	if !hasPipelineBody(prepared.clean) {
		result := CompileRes{Meta: prepared.meta}
		if cfg.actionGraph {
			result.ActionGraph = &ActionGraph{}
		}
		return result, nil
	}

	if isStrictConfig(prepared.meta) {
		ctx = WithStrict(ctx)
	}
	ctx = applySymbolsToCtx(ctx, prepared.meta)

	effectiveCfg, ctx, ptEffective := applyPreprocessContributions(ctx, cfg, opts, pt, prepared.contributions, req)

	lineMods := filterLineMods(mt, effectiveCfg.lineMods)
	ast, err := NewParserWithFileOffset(ctx, ot, ptEffective, prepared.clean, prepared.name, 0).
		WithLineModifiers(lineMods...).
		Parse()
	if err != nil {
		return CompileRes{}, err
	}
	for _, validate := range effectiveCfg.validators {
		if validate == nil {
			continue
		}
		if validationErr := validate(ast, prepared.meta); validationErr != nil {
			return CompileRes{}, validationErr
		}
	}
	for _, validate := range effectiveCfg.contextValidators {
		if validate == nil {
			continue
		}
		if validationErr := validate(ctx, ast, prepared.meta); validationErr != nil {
			return CompileRes{}, validationErr
		}
	}
	if refErr := validateConfigRefs(ast, effectiveCfg.config, effectiveCfg.cliArgs); refErr != nil {
		return CompileRes{}, refErr
	}
	if analyzeErr := Analyze(resolver, ast); analyzeErr != nil {
		return CompileRes{}, analyzeErr
	}

	program, err := buildProgram(ctx, resolver, mt, ast, effectiveCfg, prepared.meta)
	if err != nil {
		return CompileRes{}, err
	}

	var graph *ActionGraph
	if effectiveCfg.actionGraph {
		graph, err = BuildActionGraph(resolver, ast)
		if err != nil {
			return CompileRes{}, err
		}
	}

	return CompileRes{Program: program, Meta: prepared.meta, AST: ast, ActionGraph: graph}, nil
}

func applySymbolsToCtx(ctx context.Context, meta map[string]any) context.Context {
	syms := SymbolsFromMeta(meta)
	if len(syms) == 0 {
		return ctx
	}
	return WithLocalSymbols(ctx, syms)
}

// hasPipelineBody reports whether src contains at least one non-EOF
// token. Whitespace, `#` line comments, and `//` line comments are
// skipped by the lexer, so a source that consists only of those is
// correctly identified as body-less.
func hasPipelineBody(src string) bool {
	lex := NewLexer(src)
	return lex.Next().Type != TokEOF
}

func applyConfigToCtx(ctx context.Context, cfg *compileConfig) context.Context {
	if cfg.bundleSources != nil {
		ctx = WithSourceLoader(ctx, cfg.bundleSources.withFallback(sourceLoaderFrom(ctx)))
	}
	if len(cfg.config) == 0 && len(cfg.cliArgs) == 0 {
		return ctx
	}
	return WithCompileConfig(ctx, cfg.config, cfg.cliArgs)
}

func applyPreprocessContributions(
	ctx context.Context,
	cfg *compileConfig,
	opts []CompileOption,
	pt *PrimaryExtensionTable,
	contributions PreprocessContributions,
	req CompileReq,
) (*compileConfig, context.Context, *PrimaryExtensionTable) {
	effectiveCfg := cfg
	if len(contributions.CompileOpts) > 0 {
		combinedOpts := make([]CompileOption, 0, len(opts)+len(contributions.CompileOpts))
		combinedOpts = append(combinedOpts, opts...)
		combinedOpts = append(combinedOpts, contributions.CompileOpts...)
		effectiveCfg = applyCompileOptions(combinedOpts)
		ctx = applyConfigToCtx(ctx, effectiveCfg)
	}

	ptEffective := pt
	if len(contributions.Primaries) > 0 || len(req.InheritedPrimaries) > 0 {
		var all []PrimaryExtension
		if pt != nil {
			all = append(all, pt.All()...)
		}
		all = append(all, req.InheritedPrimaries...)
		all = append(all, contributions.Primaries...)
		ptEffective = NewPrimaryExtensionTable(all...)
	}

	return effectiveCfg, ctx, ptEffective
}

func buildProgram(
	ctx context.Context,
	resolver CapabilityResolver,
	mt *ModifierTable,
	ast Expr,
	cfg *compileConfig,
	meta map[string]any,
) (action.AnyAction, error) {
	advisors := cfg.advisors
	if len(cfg.argSchemas) > 0 {
		advisors = append(
			[]AtomAdviseFunc{argSchemaAdvisor(resolver, cfg.argSchemas)},
			advisors...,
		)
	}

	program, err := buildAction(ctx, resolver, mt, ast, cfg.actionIntrospection, advisors...)
	if err != nil {
		return nil, err
	}

	for _, wrap := range cfg.wrappers {
		if wrap == nil {
			continue
		}
		wrapped, werr := wrap(meta, program)
		if werr != nil {
			return nil, werr
		}
		if wrapped != nil {
			if cfg.actionIntrospection {
				wrapped = action.WithComposition(wrapped, action.IntrospectionComposition{
					Kind: "pipeline_wrapper", Opaque: true,
					Notes: []string{"wrapper internals may be closure-captured and are not expanded"},
					Children: []action.IntrospectionChild{{
						Node: program, Relation: IntrospectionKindWraps, Role: "program",
					}},
				})
			}
			program = wrapped
		}
	}
	return program, nil
}

func Library(
	resolver CapabilityResolver,
	dt *DirectiveTable,
	mt *ModifierTable,
	ot *OperatorTable,
	pt *PrimaryExtensionTable,
	opts ...CompileOption,
) action.Library {
	lib := action.Library{
		Name:        "github.com/nexssp/flow/compiler",
		Description: "Action-based Nexss Flow compiler core",
		Actions: []action.AnyAction{
			CompileAction(resolver, dt, mt, ot, pt, opts...),
			RunAction(),
			CompileAndRun(resolver, dt, mt, ot, pt, opts...),
		},
	}
	if dt != nil {
		lib.Actions = append(lib.Actions, dt.Actions()...)
	}
	if mt != nil {
		lib.Actions = append(lib.Actions, mt.Actions()...)
	}
	if ot != nil {
		lib.Actions = append(lib.Actions, ot.Actions()...)
	}
	return lib
}
