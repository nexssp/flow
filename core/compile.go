package core

import (
	"context"
	"strings"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type CompileReq struct {
	Source             string
	Name               string
	Line               int
	InheritedPrimaries []PrimaryExtension
}

type CompileRes struct {
	Program action.AnyAction
	Meta    map[string]any
	AST     Expr
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
		if strings.TrimSpace(req.Source) == "" {
			return CompileRes{}, xerr.BadRequest("compile: source is empty")
		}

		if len(cfg.config) > 0 || len(cfg.cliArgs) > 0 {
			ctx = WithCompileConfig(ctx, cfg.config, cfg.cliArgs)
		}

		clean, meta, err := Preprocess(ctx, dt, req.Source, req.Name)
		if err != nil {
			return CompileRes{}, err
		}

		if isStrictConfig(meta) {
			ctx = WithStrict(ctx)
		}

		var extraPrimaries []PrimaryExtension
		var extraOpts []CompileOption
		for _, fn := range cfg.onPreprocess {
			contribs := fn(meta)
			extraPrimaries = append(extraPrimaries, contribs.Primaries...)
			extraOpts = append(extraOpts, contribs.CompileOpts...)
		}

		effectiveCfg := cfg
		if len(extraOpts) > 0 {
			effectiveCfg = applyCompileOptions(append(opts, extraOpts...))
			if len(effectiveCfg.config) > 0 || len(effectiveCfg.cliArgs) > 0 {
				ctx = WithCompileConfig(ctx, effectiveCfg.config, effectiveCfg.cliArgs)
			}
		}

		ptEffective := pt
		if len(extraPrimaries) > 0 || len(req.InheritedPrimaries) > 0 {
			var all []PrimaryExtension
			if pt != nil {
				all = append(all, pt.All()...)
			}
			all = append(all, req.InheritedPrimaries...)
			all = append(all, extraPrimaries...)
			ptEffective = NewPrimaryExtensionTable(all...)
		}

		ast, err := NewParserWithFileOffset(ctx, ot, ptEffective, clean, req.Name, 0).Parse()
		if err != nil {
			return CompileRes{}, err
		}
		if analyzeErr := Analyze(resolver, ast); analyzeErr != nil {
			return CompileRes{}, analyzeErr
		}

		program, err := Build(ctx, resolver, mt, ast, effectiveCfg.advisors...)
		if err != nil {
			return CompileRes{}, err
		}

		for _, wrap := range effectiveCfg.wrappers {
			if wrap == nil {
				continue
			}
			wrapped, werr := wrap(meta, program)
			if werr != nil {
				return CompileRes{}, werr
			}
			if wrapped != nil {
				program = wrapped
			}
		}

		return CompileRes{Program: program, Meta: meta, AST: ast}, nil
	}).
		Description("Compile .nflow source into a runnable action").
		Tag("core", "compiler").
		Build()
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
		Name:        "github.com/nexssp/flow/core",
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
