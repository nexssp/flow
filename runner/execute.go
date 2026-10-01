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
	CompileDuration time.Duration
	RunDuration     time.Duration

	CompileAllocs     uint64
	CompileAllocBytes uint64
	RunAllocs         uint64
	RunAllocBytes     uint64
}

func readMemStats(dst *runtime.MemStats) {
	runtime.ReadMemStats(dst)
}

var defaultRunAction = core.RunAction()

// Execute compiles and runs a .nflow source under the given configuration.
func Execute(ctx context.Context, cfg Config, src, name string, payload map[string]any) (Execution, error) {
	// The true compile phase begins at the very first line of Execute.
	compileStart := time.Now()

	var startStats, compileStats, runStats runtime.MemStats
	if cfg.MeasureAllocs {
		readMemStats(&startStats)
	}

	resolver, err := newExecutionResolver(cfg.Resolver, cfg.Hooks)
	if err != nil {
		return Execution{}, err
	}

	_, meta, err := core.Preprocess(ctx, cfg.Directives, src, name)
	if err != nil {
		return Execution{}, err
	}

	topContribs := core.PreprocessContributionsFromMeta(meta, cfg.CompileOpts...)

	compileSub := func(subName, subSource string) (action.AnyAction, error) {
		subRes, subErr := core.CompileAction(
			resolver, cfg.Directives, cfg.Modifiers,
			cfg.Operators, cfg.Primaries, cfg.CompileOpts...,
		).Do(ctx, core.CompileReq{
			Source:             subSource,
			Name:               subName,
			InheritedPrimaries: topContribs.Primaries,
		})
		if subErr != nil {
			return nil, subErr
		}
		return subRes.Program, nil
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
			return Execution{Meta: meta}, fmt.Errorf("materialize: %w", matErr)
		}
	}

	compileCfg := cfg
	compileCfg.Resolver = resolver
	ctx = contracts.WithCompiler(ctx, compilerAdapter{cfg: compileCfg})

	// Inject the dynamic resolver directly instead of building a static registry
	ctx = contracts.WithActionResolver(ctx, resolver)

	compileAct := core.CompileAction(
		resolver, cfg.Directives, cfg.Modifiers,
		cfg.Operators, cfg.Primaries, cfg.CompileOpts...,
	)

	compiledRes, err := compileAct.Do(ctx, core.CompileReq{
		Source: src,
		Name:   name,
	})

	compileDuration := time.Since(compileStart)

	if cfg.MeasureAllocs {
		readMemStats(&compileStats)
	}
	if err != nil {
		result := Execution{Meta: meta, CompileDuration: compileDuration}
		fillCompileAllocs(&result, cfg.MeasureAllocs, &startStats, &compileStats)
		return result, err
	}

	runStart := time.Now()
	runRes, err := defaultRunAction.Do(ctx, core.RunReq{
		Program: compiledRes.Program,
		Payload: payload,
	})
	runDuration := time.Since(runStart)

	if cfg.MeasureAllocs {
		readMemStats(&runStats)
	}

	result := Execution{
		Output:          runRes.Output,
		Meta:            meta,
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
