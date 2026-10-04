package runner

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/core"
)

// Execution is the outcome of one Execute call.
type Execution struct {
	Output          any
	Meta            map[string]any
	Resolver        core.CapabilityResolver
	CompileDuration time.Duration
	RunDuration     time.Duration

	CompileAllocs     uint64
	CompileAllocBytes uint64
	RunAllocs         uint64
	RunAllocBytes     uint64
}

// Compilation is the result of compiling one source without invoking its
// workflow actions. Program is nil for declaration-only sources such as a
// file containing only @pipeline definitions.
type Compilation struct {
	Program  action.AnyAction
	Meta     map[string]any
	Resolver core.CapabilityResolver
}

func readMemStats(dst *runtime.MemStats) {
	runtime.ReadMemStats(dst)
}

var defaultRunAction = core.RunAction()

func withExecutionContext(ctx context.Context, cfg Config, resolver core.CapabilityResolver) context.Context {
	cfg.Resolver = resolver
	ctx = contracts.WithCompiler(ctx, compilerAdapter{cfg: cfg})
	return contracts.WithActionResolver(ctx, resolver)
}

// Compile preprocesses source, materializes declarations, and compiles its
// top-level program without invoking workflow actions.
func Compile(ctx context.Context, cfg Config, src, name string) (Compilation, error) {
	resolver, err := newExecutionResolver(cfg.Resolver, cfg.Hooks)
	if err != nil {
		return Compilation{}, err
	}

	prepared, err := core.PrepareSource(ctx, cfg.Directives, src, name, cfg.CompileOpts...)
	if err != nil {
		return Compilation{Resolver: resolver}, err
	}

	meta := prepared.Metadata()
	topContribs := prepared.Contributions()

	// Variadic signature to accept modifiers for sub-pipelines:
	compileSub := func(subName, subSource string, mods ...string) (action.AnyAction, error) {
		wrapperMods, bodyMods := SplitPipelineModifiers(cfg.Modifiers, mods)

		subOpts := append([]core.CompileOption(nil), cfg.CompileOpts...)
		if len(bodyMods) > 0 {
			captured := append([]string(nil), bodyMods...)
			subOpts = append(subOpts, core.WithLineModifiers(core.LineLookup{
				Source: core.ModifierSource{
					Kind:  "pipeline",
					Label: subName,
				},
				Fn: func(int) []string {
					return captured
				},
			}))
		}

		subRes, subErr := core.CompileAction(
			resolver, cfg.Directives, cfg.Modifiers,
			cfg.Operators, cfg.Primaries, subOpts...,
		).Do(ctx, core.CompileReq{
			Source:             subSource,
			Name:               subName,
			InheritedPrimaries: topContribs.Primaries,
		})
		if subErr != nil {
			return nil, subErr
		}

		program := subRes.Program
		if len(wrapperMods) > 0 && cfg.Modifiers != nil {
			applied, merr := cfg.Modifiers.ApplyAll(program, wrapperMods)
			if merr != nil {
				return nil, merr
			}
			program = applied
		}

		return program, nil
	}

	for _, m := range cfg.Materializers {
		if m == nil {
			continue
		}
		if matErr := m(core.MaterializeReq{
			Ctx:      ctx,
			Meta:     meta,
			Resolver: resolver,
			Compile:  compileSub,
		}); matErr != nil {
			return Compilation{Meta: meta, Resolver: resolver}, fmt.Errorf("materialize: %w", matErr)
		}
	}

	ctx = withExecutionContext(ctx, cfg, resolver)

	compileAct := core.CompileAction(
		resolver, cfg.Directives, cfg.Modifiers,
		cfg.Operators, cfg.Primaries, cfg.CompileOpts...,
	)

	compiledRes, err := compileAct.Do(ctx, core.CompileReq{
		Prepared: prepared,
	})
	if err != nil {
		return Compilation{Meta: meta, Resolver: resolver}, err
	}
	return Compilation{
		Program:  compiledRes.Program,
		Meta:     meta,
		Resolver: resolver,
	}, nil
}

// Execute compiles and runs a .nflow source under the given configuration.
func Execute(ctx context.Context, cfg Config, src, name string, payload map[string]any) (Execution, error) {
	compileStart := time.Now()

	var startStats, compileStats, runStats runtime.MemStats
	if cfg.MeasureAllocs {
		readMemStats(&startStats)
	}

	compiled, err := Compile(ctx, cfg, src, name)
	compileDuration := time.Since(compileStart)
	if cfg.MeasureAllocs {
		readMemStats(&compileStats)
	}
	if err != nil {
		result := Execution{Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, err
	}
	if compiled.Program == nil {
		result := Execution{Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, errors.New("compile: source is empty")
	}

	ctx = withExecutionContext(ctx, cfg, compiled.Resolver)
	runStart := time.Now()
	runRes, err := defaultRunAction.Do(ctx, core.RunReq{
		Program: compiled.Program,
		Payload: payload,
	})
	runDuration := time.Since(runStart)

	if cfg.MeasureAllocs {
		readMemStats(&runStats)
	}

	result := Execution{
		Output:          runRes.Output,
		Meta:            compiled.Meta,
		Resolver:        compiled.Resolver,
		CompileDuration: compileDuration,
		RunDuration:     runDuration,
	}
	fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
	fillRunAllocs(&result, cfg.MeasureAllocs, &compileStats, &runStats)
	if err != nil {
		return result, err
	}
	return result, nil
}

func fillCompileAllocs(dst *Execution, enabled bool, before, after *runtime.MemStats) {
	if !enabled {
		return
	}
	dst.CompileAllocs = after.Mallocs - before.Mallocs
	dst.CompileAllocBytes = after.TotalAlloc - before.TotalAlloc
}

func fillRunAllocs(dst *Execution, enabled bool, before, after *runtime.MemStats) {
	if !enabled {
		return
	}
	dst.RunAllocs = after.Mallocs - before.Mallocs
	dst.RunAllocBytes = after.TotalAlloc - before.TotalAlloc
}

type compilerAdapter struct {
	cfg Config
}

func (a compilerAdapter) CompilePipeline(expr string) (action.Executable, error) {
	res, err := core.CompileAction(
		a.cfg.Resolver, a.cfg.Directives, a.cfg.Modifiers,
		a.cfg.Operators, a.cfg.Primaries, a.cfg.CompileOpts...,
	).Do(context.Background(), core.CompileReq{Source: expr, Name: "<child>"})
	if err != nil {
		return nil, err
	}
	exec, ok := res.Program.(action.Executable)
	if !ok {
		return nil, errors.New("runner: compiled child is not Executable")
	}
	return exec, nil
}

// SplitPipelineModifiers separates a pipeline header's modifiers into
// those that belong on the wrapper action and those that propagate to
// the pipeline body. Inheritable policy modifiers (:timeout, :retry, …)
// propagate; metadata modifiers (:tag, :status, :route, …) stay on the
// wrapper.
//
// It is shared by compile-time pipeline materialization and callers that
// need the same wrapper-versus-body modifier split.
func SplitPipelineModifiers(table *core.ModifierTable, mods []string) (wrapper, body []string) {
	if len(mods) == 0 {
		return nil, nil
	}
	for _, raw := range mods {
		if table == nil {
			wrapper = append(wrapper, raw)
			continue
		}
		name := core.ModifierName(raw)
		if m, ok := table.ByName(name); ok && m.Inheritable {
			body = append(body, raw)
			continue
		}
		wrapper = append(wrapper, raw)
	}
	return wrapper, body
}

// SourcedModifier is a modifier together with the origin that produced
// it. SplitPipelineModifiersWithSources returns these so `nflow explain`
// can attribute modifiers to a pipeline or a profile.
type SourcedModifier struct {
	Raw    string
	Source core.ModifierSource
}

// SplitPipelineModifiersWithSources is SplitPipelineModifiers with the
// per-modifier origin preserved. Sources must be parallel to mods;
// missing entries default to Kind "pipeline" with no label.
func SplitPipelineModifiersWithSources(
	table *core.ModifierTable,
	mods []string,
	sources []core.ModifierSource,
) (wrapper, body []SourcedModifier) {
	for i, raw := range mods {
		src := core.ModifierSource{Kind: "pipeline"}
		if i < len(sources) {
			src = sources[i]
		}
		entry := SourcedModifier{Raw: raw, Source: src}
		if table != nil {
			if mod, ok := table.ByName(core.ModifierName(raw)); ok && mod.Inheritable {
				body = append(body, entry)
				continue
			}
		}
		wrapper = append(wrapper, entry)
	}
	return wrapper, body
}
