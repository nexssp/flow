package flow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/directives/core"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xerr"
)

type GraphExecReq struct {
	DSL            string         `json:"dsl,omitempty" cli:"dsl,d" usage:"Compact arrow pipeline expression"`
	YAML           string         `json:"yaml,omitempty" cli:"yaml,y" usage:"Declarative YAML graph manifest"`
	Profile        Profile        `json:"profile,omitempty" usage:"Security envelope: trusted | untrusted_input | network_isolated"`
	Name           string         `json:"name,omitempty" usage:"Source identifier for error messages (usually the .flow path)"`
	InitialPayload map[string]any `json:"initial_payload,omitempty" usage:"Initial state values passed to root nodes"`

	// Preprocessed carries every declaration the runner parsed from
	// the source file. Extension points (atom advisors, pipeline
	// wrappers) read their own declarations from it.
	Preprocessed *core.Preprocessed `json:"-"`
}

type GraphExecRes struct {
	GraphName  string         `json:"graph_name"`
	Outputs    map[string]any `json:"outputs"`
	Result     any            `json:"result,omitempty"`
	LayersRun  int            `json:"layers_run"`
	DurationMS int64          `json:"duration_ms"`
}

func NewExecuteAction(comp *Compiler) *action.BuiltAction[GraphExecReq, GraphExecRes] {
	return action.New("graph.execute", func(ctx context.Context, req GraphExecReq) (GraphExecRes, error) {
		start := time.Now()

		if req.YAML != "" {
			compiled, err := LoadYAML([]byte(req.YAML))
			if err != nil {
				return GraphExecRes{}, err
			}
			return runAsDAG(ctx, req, comp, compiled.Definition, start)
		}

		if req.DSL == "" {
			return GraphExecRes{}, xerr.BadRequest(
				"graph.execute: either 'dsl' or 'yaml' must be supplied")
		}

		// If the file declared directives that modify pipeline execution
		// (like @on_error, @fallback, @retry), it MUST run through the pipeline compiler.
		if hasPipelineDirectives(req.Preprocessed) {
			return runAsPipeline(ctx, req, comp, start)
		}

		// Stream pipelines (fs.walk -> fs.read -> out.file, …) look like
		// ordinary atom chains to ParseArrowDSL, but they must go through
		// the pipeline compiler, which knows how to compose stream sources
		// and operators. Detect them here so the DAG path never tries to
		// resolve a stream atom as a unary action.
		effectiveRegistry := DefaultRegistry()
		if effectiveRegistry == nil && comp != nil && comp.registry != nil {
			effectiveRegistry = RegistryFromActionRegistry(comp.registry)
		}
		if HasStreamAtoms(req.DSL, effectiveRegistry) {
			return runAsPipeline(ctx, req, comp, start)
		}

		def, err := ParseArrowDSL(req.Name, req.DSL)
		if err != nil {
			var npe *compiler.NeedsPipelineError
			if errors.As(err, &npe) {
				return runAsPipeline(ctx, req, comp, start)
			}
			return GraphExecRes{}, err
		}

		if req.Profile != "" {
			def.Profile = req.Profile
		}

		return runAsDAG(ctx, req, comp, def, start)
	}).
		Description("Compiles and executes graph pipelines with budget bounds, capability enforcement, and branch recording").
		Tag("graph", "orchestration", "ai").
		Build()
}

func hasPipelineDirectives(pre *core.Preprocessed) bool {
	if pre == nil || len(pre.Declarations) == 0 {
		return false
	}
	// Check if any registered wrapper or policy is declared in the file
	for _, w := range core.PipelineWrappers() {
		if _, ok := pre.Declarations[w.Name()]; ok {
			return true
		}
	}
	for _, p := range core.AtomPolicies() {
		if _, ok := pre.Declarations[p.Name()]; ok {
			return true
		}
	}
	return false
}

func runAsDAG(
	ctx context.Context,
	req GraphExecReq,
	comp *Compiler,
	def GraphDefinition,
	start time.Time,
) (GraphExecRes, error) {
	dagInstance, compiledGraph, err := comp.Compile(ctx, def)
	if err != nil {
		return GraphExecRes{}, fmt.Errorf("compile graph: %w", err)
	}

	state := NewState(req.InitialPayload)

	dagState := AcquireStateFromGraphState(state)
	defer dagState.Release()

	finalDagState, err := dagInstance.Execute(ctx, dagState.AsRead())
	if finalDagState != nil {
		defer finalDagState.Release()
	}
	if err != nil {
		return GraphExecRes{}, err
	}

	var final any
	if len(compiledGraph.Layers) > 0 {
		last := compiledGraph.Layers[len(compiledGraph.Layers)-1]
		if len(last) > 0 {
			final = finalDagState.Data()[dag.OutputKey(last[len(last)-1])]
		}
	}

	return GraphExecRes{
		GraphName:  def.Metadata.Name,
		Outputs:    finalDagState.Data(),
		Result:     final,
		LayersRun:  len(compiledGraph.Layers),
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func runAsPipeline(
	ctx context.Context,
	req GraphExecReq,
	comp *Compiler,
	start time.Time,
) (GraphExecRes, error) {
	if comp == nil || comp.registry == nil {
		return GraphExecRes{}, xerr.Internal("graph.execute: pipeline fallback requires a registry")
	}

	ctx = contracts.WithRegistry(ctx, comp.registry)
	ctx = contracts.WithCompiler(ctx, comp)

	compileOpts := []CompileOption{}
	if req.Preprocessed != nil {
		resolvedCfg := BuildResolvedConfig(req.Preprocessed, nil)
		compileOpts = append(compileOpts,
			WithConfig(resolvedCfg),
			WithAtomAdvisors(req.Preprocessed),
			WithSchemas(req.Preprocessed),
		)
	}

	//nolint:contextcheck // ctx threaded via compileOpts -> WithCompileContext
	bld, err := CompilePipeline(req.DSL, comp.registry, compileOpts...)
	if err != nil {
		return GraphExecRes{}, err
	}

	if len(comp.hooks) > 0 {
		bld = bld.AnyHook(comp.hooks...)
	}

	built := action.Executable(bld.Build())

	if req.Preprocessed != nil {
		for _, wrapper := range core.PipelineWrappers() {
			wrapped, wrapErr := wrapper.WrapPipeline(req.Preprocessed, built)
			if wrapErr != nil {
				return GraphExecRes{}, fmt.Errorf("@%s wrapper: %w", wrapper.Name(), wrapErr)
			}
			if wrapped != nil {
				built = wrapped
			}
		}
	}

	out, err := action.InvokeAny(ctx, built, req.InitialPayload)
	if err != nil {
		return GraphExecRes{}, err
	}

	outputs := map[string]any{}
	if m, ok := out.(map[string]any); ok {
		outputs = m
	} else {
		outputs["result"] = out
	}

	return GraphExecRes{
		GraphName:  req.Name,
		Outputs:    outputs,
		Result:     out,
		LayersRun:  1,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func Execute[Req, Res any](ctx context.Context, act *action.BuiltAction[Req, Res], req Req) (Res, error) {
	return act.Do(ctx, req)
}
