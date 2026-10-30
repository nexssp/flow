package runner

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/compiler/contracts"
)

type Execution struct {
	ExecutionID     string
	Output          any
	Suspension      *ApprovalSuspension
	Meta            map[string]any
	Resolver        compiler.CapabilityResolver
	CompileDuration time.Duration
	RunDuration     time.Duration

	CompileAllocs     uint64
	CompileAllocBytes uint64
	RunAllocs         uint64
	RunAllocBytes     uint64
}

type Compilation struct {
	Program  action.AnyAction
	Meta     map[string]any
	Resolver compiler.CapabilityResolver
}

type EntryPoint struct {
	Name    string
	Payload any
}

func readMemStats(dst *runtime.MemStats) {
	runtime.ReadMemStats(dst)
}

var defaultRunAction = compiler.RunAction()

func withExecutionContext(ctx context.Context, cfg Config, resolver compiler.CapabilityResolver, meta map[string]any) context.Context {
	cfg.Resolver = resolver
	ctx = contracts.WithCompiler(ctx, compilerAdapter{cfg: cfg, inheritedMeta: meta})
	ctx = contracts.WithActionResolver(ctx, resolver)
	return contracts.WithFileResolver(ctx, cfg.FileResolver)
}

func Compile(ctx context.Context, cfg Config, src, name string) (Compilation, error) {
	return CompileWithOptions(ctx, cfg, src, name)
}

func CompileWithOptions(ctx context.Context, cfg Config, src, name string, opts ...compiler.CompileOption) (Compilation, error) {
	if len(opts) > 0 {
		cfg.CompileOpts = append(append([]compiler.CompileOption(nil), cfg.CompileOpts...), opts...)
	}
	prepared, err := compiler.PrepareSource(ctx, cfg.Directives, src, name, cfg.CompileOpts...)
	if err != nil {
		return Compilation{}, err
	}
	return compilePrepared(ctx, cfg, prepared)
}

// CompilePrepared compiles a source that has already completed preprocessing.
// It is useful when a caller must inspect the same directive metadata before
// compiling and should not repeat the preprocessing pass.
func CompilePrepared(ctx context.Context, cfg Config, prepared *compiler.PreparedSource) (Compilation, error) {
	if prepared == nil {
		return Compilation{}, errors.New("runner: prepared source is required")
	}
	return compilePrepared(ctx, cfg, prepared)
}

func Preflight(ctx context.Context, cfg Config, src, name string) (*compiler.PreparedSource, error) {
	return compiler.PrepareSource(ctx, cfg.Directives, src, name, cfg.CompileOpts...)
}

func ExecutePrepared(
	ctx context.Context,
	cfg Config,
	prepared *compiler.PreparedSource,
	entry EntryPoint,
	extraOpts ...compiler.CompileOption,
) (Execution, error) {
	ctx, executionID, identityErr := ensureExecutionIdentity(ctx)
	if identityErr != nil {
		return Execution{}, identityErr
	}
	if len(extraOpts) > 0 {
		cfg.CompileOpts = append(append([]compiler.CompileOption{}, cfg.CompileOpts...), extraOpts...)
	}

	compileStart := time.Now()
	var startStats, compileStats, runStats runtime.MemStats
	if cfg.MeasureAllocs {
		readMemStats(&startStats)
	}

	compiled, err := compilePrepared(ctx, cfg, prepared)
	compileDuration := time.Since(compileStart)
	if cfg.MeasureAllocs {
		readMemStats(&compileStats)
	}
	if err != nil {
		result := Execution{ExecutionID: executionID, Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, err
	}
	if entry.Name == "" {
		result := Execution{ExecutionID: executionID, Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, errors.New("runner: entrypoint name is required")
	}

	entryAction, ok := compiled.Resolver.Action(entry.Name)
	if !ok {
		result := Execution{ExecutionID: executionID, Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, fmt.Errorf("runner: entrypoint %q is not registered in the compiled source", entry.Name)
	}

	if syms := compiler.SymbolsFromMeta(compiled.Meta); len(syms) > 0 {
		ctx = compiler.WithLocalSymbols(ctx, syms)
	}

	ctx = withExecutionContext(ctx, cfg, compiled.Resolver, compiled.Meta)

	runStart := time.Now()
	preparedName, preparedSourceHash := prepared.SourceIdentity()
	output, suspension, runErr := runApprovalAware(
		ctx,
		cfg.Journal,
		executionID,
		preparedSourceHash,
		preparedName,
		entry.Payload,
		prepared.Metadata(),
		func() (any, error) {
			return invokeWithRunScope(ctx, func(runCtx context.Context) (any, error) {
				return action.InvokeAny(runCtx, entryAction, entry.Payload)
			})
		},
	)
	runDuration := time.Since(runStart)
	if cfg.MeasureAllocs {
		readMemStats(&runStats)
	}

	result := Execution{
		ExecutionID:     executionID,
		Output:          output,
		Suspension:      suspension,
		Meta:            compiled.Meta,
		Resolver:        compiled.Resolver,
		CompileDuration: compileDuration,
		RunDuration:     runDuration,
	}
	fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
	fillRunAllocs(&result, cfg.MeasureAllocs, &compileStats, &runStats)
	if runErr != nil {
		return result, runErr
	}
	return result, nil
}

func compilePrepared(ctx context.Context, cfg Config, prepared *compiler.PreparedSource) (Compilation, error) {
	cfg.CompileOpts = compileOptsWithRunnerPolicies(cfg)
	return compileSource(ctx, cfg, prepared)
}

func compileSource(ctx context.Context, cfg Config, prepared *compiler.PreparedSource) (Compilation, error) {
	ctx = compiler.WithLocalSymbols(ctx, compiler.SymbolsFromMeta(prepared.Metadata()))
	resolver, err := newExecutionResolver(cfg.Resolver, cfg.Hooks)
	if err != nil {
		return Compilation{}, err
	}

	for _, imported := range compiler.SourceImports(prepared.Metadata()) {
		child, childErr := compileSource(ctx, cfg, imported.Source)
		if childErr != nil {
			return Compilation{}, compiler.SourceError(imported.Position, "@include %s: %w", imported.Namespace, childErr)
		}
		if exportErr := mountSourceModule(cfg, resolver, imported, child); exportErr != nil {
			return Compilation{}, exportErr
		}
	}

	meta := prepared.Metadata()
	if syms := compiler.SymbolsFromMeta(meta); len(syms) > 0 {
		ctx = compiler.WithLocalSymbols(ctx, syms)
	}
	topContribs := prepared.Contributions()

	compileSub := func(subName, subSource string, mods ...string) (action.AnyAction, error) {
		wrapperMods, bodyMods := SplitPipelineModifiers(cfg.Modifiers, mods)

		subOpts := append([]compiler.CompileOption(nil), cfg.CompileOpts...)
		subOpts = append(subOpts, topContribs.CompileOpts...)
		if len(bodyMods) > 0 {
			captured := append([]string(nil), bodyMods...)
			subOpts = append(subOpts, compiler.WithLineModifiers(compiler.LineLookup{
				Source: compiler.ModifierSource{
					Kind:  "pipeline",
					Label: subName,
				},
				Fn: func(int) []string {
					return captured
				},
			}))
		}

		subRes, subErr := compiler.CompileAction(
			resolver, cfg.Directives, cfg.Modifiers,
			cfg.Operators, cfg.Primaries, subOpts...,
		).Do(ctx, compiler.CompileReq{
			Source:             subSource,
			Name:               subName,
			InheritedPrimaries: topContribs.Primaries,
			InheritedMeta:      meta,
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
			if _, introspected := program.(action.IntrospectionProvider); introspected {
				applied = action.WithComposition(applied, action.IntrospectionComposition{
					Kind: "pipeline_modifier", Opaque: true,
					Notes: []string{"modifier values are omitted"},
					Children: []action.IntrospectionChild{{
						Node: program, Relation: compiler.IntrospectionKindWraps, Role: "program",
					}},
				})
			}
			program = applied
		}

		return program, nil
	}

	for _, m := range cfg.Materializers {
		if m == nil {
			continue
		}
		if matErr := m(compiler.MaterializeReq{
			Ctx:           ctx,
			Meta:          meta,
			Resolver:      resolver,
			Compile:       compileSub,
			Introspection: prepared.ActionIntrospectionEnabled(),
		}); matErr != nil {
			return Compilation{Meta: meta, Resolver: resolver}, fmt.Errorf("materialize: %w", matErr)
		}
	}

	ctx = withExecutionContext(ctx, cfg, resolver, meta)

	compileAct := compiler.CompileAction(
		resolver, cfg.Directives, cfg.Modifiers,
		cfg.Operators, cfg.Primaries, cfg.CompileOpts...,
	)

	compiledRes, err := compileAct.Do(ctx, compiler.CompileReq{
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

func Execute(ctx context.Context, cfg Config, src, name string, payload map[string]any) (Execution, error) {
	ctx, executionID, identityErr := ensureExecutionIdentity(ctx)
	if identityErr != nil {
		return Execution{}, identityErr
	}
	compileStart := time.Now()

	var startStats, compileStats runtime.MemStats
	if cfg.MeasureAllocs {
		readMemStats(&startStats)
	}

	compiled, err := Compile(ctx, cfg, src, name)
	compileDuration := time.Since(compileStart)
	if cfg.MeasureAllocs {
		readMemStats(&compileStats)
	}
	if err != nil {
		result := Execution{ExecutionID: executionID, Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, err
	}
	return executeCompiled(ctx, cfg, compiled, src, name, payload, executionID, compileDuration, &startStats, &compileStats, &compileStats)
}

// ExecuteCompiled executes a previously compiled program without compiling it again.
// cfg, src, and name must match the values used to create compiled; src and name
// are used for approval identity. The compilation's program, resolver, and other
// retained state are reused intentionally, so this is warm repeated execution,
// not a process-cold-start benchmark. Each call gets its own execution identity.
func ExecuteCompiled(ctx context.Context, cfg Config, compiled Compilation, src, name string, payload map[string]any) (Execution, error) {
	ctx, executionID, identityErr := ensureExecutionIdentity(ctx)
	if identityErr != nil {
		return Execution{}, identityErr
	}
	var runStartStats runtime.MemStats
	if cfg.MeasureAllocs {
		readMemStats(&runStartStats)
	}
	return executeCompiled(ctx, cfg, compiled, src, name, payload, executionID, 0, nil, nil, &runStartStats)
}

func executeCompiled(
	ctx context.Context,
	cfg Config,
	compiled Compilation,
	src, name string,
	payload map[string]any,
	executionID string,
	compileDuration time.Duration,
	compileStartStats, compileEndStats, runStartStats *runtime.MemStats,
) (Execution, error) {
	if compiled.Program == nil {
		result := Execution{ExecutionID: executionID, Meta: compiled.Meta, Resolver: compiled.Resolver, CompileDuration: compileDuration}
		if compileStartStats != nil && compileEndStats != nil {
			fillCompileAllocs(&result, cfg.MeasureAllocs, compileStartStats, compileEndStats)
		}
		return result, xerr.BadRequest("compile: no runnable Flow expression remained after preprocessing. Here, source means the .nflow program text, not a JSON payload field or symbol. A declaration such as @pipeline NAME defines a reusable action but does not execute it; add a top-level call such as `{} -> pipeline.NAME`. If the file already has a top-level call, inspect what Flow parsed with `nflow info <file>` and confirm the command is running the saved file you inspected.")
	}
	var runStats runtime.MemStats

	if syms := compiler.SymbolsFromMeta(compiled.Meta); len(syms) > 0 {
		ctx = compiler.WithLocalSymbols(ctx, syms)
	}

	ctx = withExecutionContext(ctx, cfg, compiled.Resolver, compiled.Meta)
	runStart := time.Now()
	output, suspension, err := runApprovalAware(
		ctx,
		cfg.Journal,
		executionID,
		sourceDigest(src),
		name,
		payload,
		compiled.Meta,
		func() (any, error) {
			return invokeWithRunScope(ctx, func(runCtx context.Context) (any, error) {
				runRes, runErr := defaultRunAction.Do(runCtx, compiler.RunReq{
					Program: compiled.Program,
					Payload: payload,
				})
				return runRes.Output, runErr
			})
		},
	)
	runDuration := time.Since(runStart)

	if cfg.MeasureAllocs {
		readMemStats(&runStats)
	}

	result := Execution{
		ExecutionID:     executionID,
		Output:          output,
		Suspension:      suspension,
		Meta:            compiled.Meta,
		Resolver:        compiled.Resolver,
		CompileDuration: compileDuration,
		RunDuration:     runDuration,
	}
	if cfg.MeasureAllocs {
		if compileStartStats != nil && compileEndStats != nil {
			fillCompileAllocs(&result, true, compileStartStats, compileEndStats)
		}
		fillRunAllocs(&result, true, runStartStats, &runStats)
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func invokeWithRunScope(ctx context.Context, invoke func(context.Context) (any, error)) (output any, err error) {
	runCtx, scope := compiler.NewRunScope(ctx)
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = scope.Close(runCtx)
			panic(recovered)
		}
	}()
	output, err = invoke(runCtx)
	return output, errors.Join(err, scope.Close(runCtx))
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
	cfg           Config
	inheritedMeta map[string]any
}

func (a compilerAdapter) CompilePipeline(expr string) (action.Executable, error) {
	res, err := compiler.CompileAction(
		a.cfg.Resolver, a.cfg.Directives, a.cfg.Modifiers,
		a.cfg.Operators, a.cfg.Primaries, a.cfg.CompileOpts...,
	).Do(context.Background(), compiler.CompileReq{Source: expr, Name: "<child>", InheritedMeta: a.inheritedMeta})
	if err != nil {
		return nil, err
	}
	exec, ok := res.Program.(action.Executable)
	if !ok {
		return nil, errors.New("runner: compiled child is not Executable")
	}
	return exec, nil
}

func SplitPipelineModifiers(table *compiler.ModifierTable, mods []string) (wrapper, body []string) {
	if len(mods) == 0 {
		return nil, nil
	}
	for _, raw := range mods {
		if table == nil {
			wrapper = append(wrapper, raw)
			continue
		}
		name := compiler.ModifierName(raw)
		if m, ok := table.ByName(name); ok && m.Inheritable {
			body = append(body, raw)
			continue
		}
		wrapper = append(wrapper, raw)
	}
	return wrapper, body
}

type SourcedModifier struct {
	Raw    string
	Source compiler.ModifierSource
}

func SplitPipelineModifiersWithSources(
	table *compiler.ModifierTable,
	mods []string,
	sources []compiler.ModifierSource,
) (wrapper, body []SourcedModifier) {
	for i, raw := range mods {
		src := compiler.ModifierSource{Kind: "pipeline"}
		if i < len(sources) {
			src = sources[i]
		}
		entry := SourcedModifier{Raw: raw, Source: src}
		if table != nil {
			if mod, ok := table.ByName(compiler.ModifierName(raw)); ok && mod.Inheritable {
				body = append(body, entry)
				continue
			}
		}
		wrapper = append(wrapper, entry)
	}
	return wrapper, body
}
