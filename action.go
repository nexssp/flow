package flow

import (
	"context"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/transport/tcli"
	"github.com/nexssp/transport/thttp"
	"github.com/nexssp/transportai/ta2a"
)

// GraphExecReq defines the omni-protocol input payload.
type GraphExecReq struct {
	DSL            string         `json:"dsl,omitempty" cli:"dsl,d" usage:"Compact arrow pipeline expression"`
	YAML           string         `json:"yaml,omitempty" cli:"yaml,y" usage:"Declarative YAML graph manifest"`
	InitialPayload map[string]any `json:"initial_payload,omitempty" usage:"Initial state values passed to root nodes"`
}

// GraphExecRes defines the structured execution audit output.
type GraphExecRes struct {
	GraphName  string         `json:"graph_name"`
	Outputs    map[string]any `json:"outputs"`
	LayersRun  int            `json:"layers_run"`
	DurationMS int64          `json:"duration_ms"`
}

// NewExecuteAction exposes dynamic graph execution over CLI, HTTP REST, MCP, and A2A.
func NewExecuteAction(compiler *Compiler) *action.BuiltAction[GraphExecReq, GraphExecRes] {
	return action.New("graph.execute", func(ctx context.Context, req GraphExecReq) (GraphExecRes, error) {
		start := time.Now()

		var (
			def GraphDefinition
			err error
		)

		switch {
		case req.YAML != "":
			compiled, loadErr := LoadYAML([]byte(req.YAML))
			if loadErr != nil {
				return GraphExecRes{}, loadErr
			}

			def = compiled.Definition

		case req.DSL != "":
			def, err = ParseArrowDSL("arrow_pipeline", req.DSL)
			if err != nil {
				return GraphExecRes{}, err
			}

		default:
			return GraphExecRes{}, xerr.BadRequest("graph.execute: either 'dsl' or 'yaml' must be supplied")
		}

		dagInstance, compiledGraph, err := compiler.Compile(ctx, def)
		if err != nil {
			return GraphExecRes{}, fmt.Errorf("compile graph: %w", err)
		}

		state := NewState(req.InitialPayload)

		dagState := AcquireStateFromGraphState(state)
		defer dagState.Release()

		finalDagState, err := dagInstance.Execute(ctx, dagState)
		if err != nil {
			return GraphExecRes{}, err
		}
		defer finalDagState.Release()

		return GraphExecRes{
			GraphName:  def.Metadata.Name,
			Outputs:    finalDagState.Data(),
			LayersRun:  len(compiledGraph.Layers),
			DurationMS: time.Since(start).Milliseconds(),
		}, nil
	}).
		Description("Compiles and executes multi-agent graph pipelines with budget bounds and branch recording").
		Tag("graph", "orchestration", "ai").
		Route(
			tcli.Command("graph:exec", "Execute graph manifest or arrow expression"),
			thttp.POST("/api/v1/graph/execute"),
			ta2a.Role("graph-runner").WithDescription("Autonomous Graph Workflow Runner"),
		).
		Build()
}

// Execute is a type-safe generic invoker helper.
func Execute[Req, Res any](ctx context.Context, act *action.BuiltAction[Req, Res], req Req) (Res, error) {
	return act.Do(ctx, req)
}
