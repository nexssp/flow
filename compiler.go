package flow

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/nexssp/cost"
	kernelcost "github.com/nexssp/cost/adapters/kernel"
	"github.com/nexssp/flow/journal"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/transport/codec"
)

type ApprovalGate interface {
	Check(ctx context.Context, actionName, argsJSON, token string) error
}

type Compiler struct {
	registry Registry
	journal  journal.BranchJournal
	gate     ApprovalGate
	reserver cost.Reserver
}

func NewCompiler(reg Registry, opts ...func(*Compiler)) *Compiler {
	c := &Compiler{
		registry: reg,
		journal:  journal.NewMemoryBranchJournal(),
	}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

func WithJournal(j journal.BranchJournal) func(*Compiler) {
	return func(c *Compiler) {
		if j != nil {
			c.journal = j
		}
	}
}

func WithApprovalGate(g ApprovalGate) func(*Compiler) {
	return func(c *Compiler) {
		if g != nil {
			c.gate = g
		}
	}
}

func WithReserver(r cost.Reserver) func(*Compiler) {
	return func(c *Compiler) {
		c.reserver = r
	}
}

func (c *Compiler) resolveCapability(capName string) (action.AnyAction, bool) {
	if c.registry == nil {
		return nil, false
	}

	if act, ok := c.registry.Get(capName); ok {
		return act, true
	}

	if bld, err := CompilePipeline(capName, c.registry); err == nil && bld != nil {
		return bld.Build(), true
	}

	return nil, false
}

func (c *Compiler) Compile(ctx context.Context, def GraphDefinition) (*dag.DAG, *CompiledGraph, error) {
	compiledDef, err := Compile(def)
	if err != nil {
		return nil, nil, err
	}

	hasApprovalRequirement := len(def.Policy.ApprovalRequiredFor) > 0
	for i := range def.Nodes {
		if def.Nodes[i].Approval {
			hasApprovalRequirement = true

			break
		}
	}

	if hasApprovalRequirement && c.gate == nil {
		return nil, nil, xerr.BadRequest(fmt.Sprintf(
			"flow: graph %q defines human approval requirements but no ApprovalGate was configured on Compiler",
			def.Metadata.Name,
		))
	}

	dagBuilder := dag.New(def.Metadata.Name)
	incomingGates := make(map[string][]string)
	incomingNodes := make(map[string][]string)

	for _, edge := range compiledDef.Edges {
		incomingNodes[edge.To] = append(incomingNodes[edge.To], edge.From)
	}

	var parallelSem chan struct{}
	if def.Policy.MaxParallelNodes > 0 {
		parallelSem = make(chan struct{}, def.Policy.MaxParallelNodes)
	}

	for i := range def.Nodes {
		nodeSpec := def.Nodes[i]

		act, ok := c.resolveCapability(nodeSpec.Capability)
		if !ok {
			return nil, nil, xerr.NotFound(fmt.Sprintf(
				"flow: capability %q required by node %q not found in registry",
				nodeSpec.Capability, nodeSpec.ID,
			))
		}

		outputKey := nodeSpec.ID + "_out"

		dagAction := action.New(nodeSpec.ID, func(execCtx context.Context, nCtx *dag.NodeContext) (any, error) {
			if parallelSem != nil {
				select {
				case <-execCtx.Done():
					return nil, execCtx.Err()
				case parallelSem <- struct{}{}:
					defer func() { <-parallelSem }()
				}
			}

			for _, gateID := range incomingGates[nodeSpec.ID] {
				if gatePassed, found := dag.GetNodeOutput[bool](nCtx.Input, gateID); found == nil && !gatePassed {
					return nil, nil
				}
			}

			payloadMap := make(map[string]any, len(nodeSpec.Params)+len(nodeSpec.InputBindings))
			for k, v := range nodeSpec.Params {
				payloadMap[k] = v
			}

			if len(nodeSpec.InputBindings) > 0 {
				for targetKey, upstreamPath := range nodeSpec.InputBindings {
					if val, found := nCtx.Input.Get(upstreamPath); found {
						payloadMap[targetKey] = val
					} else if directVal, dFound := dag.GetNodeOutput[any](nCtx.Input, upstreamPath); dFound == nil {
						payloadMap[targetKey] = directVal
					}
				}
			} else {
				if incoming := incomingNodes[nodeSpec.ID]; len(incoming) > 0 {
					for _, fromID := range incoming {
						directVal, dFound := dag.GetNodeOutput[any](nCtx.Input, fromID)
						if dFound == nil && directVal != nil {
							payloadMap[fromID] = directVal
						}
					}
				} else {
					for k, v := range nCtx.Input.Data() {
						payloadMap[k] = v
					}
				}
			}

			var payloadData []byte

			if len(payloadMap) > 0 {
				var mErr error

				payloadData, mErr = codec.Default.Marshal(payloadMap)
				if mErr != nil {
					return nil, xerr.Internal("flow: marshal node payload", mErr)
				}

				if def.Policy.MaxContextBytes > 0 && int64(len(payloadData)) > def.Policy.MaxContextBytes {
					return nil, xerr.BadRequest(fmt.Sprintf(
						"flow: node %q payload (%d bytes) exceeds max_context_bytes (%d)",
						nodeSpec.ID, len(payloadData), def.Policy.MaxContextBytes,
					))
				}
			}

			requiresApproval := nodeSpec.Approval || slices.Contains(def.Policy.ApprovalRequiredFor, nodeSpec.Effect)
			if requiresApproval {
				if gateErr := c.gate.Check(
					execCtx,
					"flow."+nodeSpec.ID,
					string(payloadData),
					xctx.ApprovalTokenFrom(execCtx),
				); gateErr != nil {
					return nil, gateErr
				}
			}

			return act.ExecuteDecoded(execCtx, func(target any) error {
				if len(payloadData) == 0 {
					return nil
				}

				if uErr := codec.Default.Unmarshal(payloadData, target); uErr != nil {
					return xerr.Internal("flow: unmarshal node payload", uErr)
				}

				return nil
			})
		})

		if nodeSpec.TimeoutMS > 0 {
			dagAction.Timeout(time.Duration(nodeSpec.TimeoutMS) * time.Millisecond)
		}

		if nodeSpec.Retry.MaxAttempts > 0 {
			dagAction.Retry(nodeSpec.Retry.MaxAttempts, action.ExponentialBackoff(100*time.Millisecond, 2*time.Second))
		}

		if c.reserver != nil && nodeSpec.EstimateMicros > 0 {
			dagAction.AnyHook(kernelcost.GuardAction(c.reserver, nodeSpec.EstimateMicros))
		}

		dagBuilder.AddNode(nodeSpec.ID, outputKey, dagAction.Build())
	}

	for _, edge := range compiledDef.Edges {
		if edge.When != "" || edge.Otherwise {
			gateID := fmt.Sprintf("gate_%s_to_%s", edge.From, edge.To)
			gateOutKey := gateID + "_out"
			sourceNode := edge.From
			targetNode := edge.To

			gateAction := action.New(gateID, func(execCtx context.Context, nCtx *dag.NodeContext) (bool, error) {
				runID := action.ExecutionIDFrom(execCtx)
				st := NewStateFromDAG(nCtx.Input)

				var (
					selectedEdges []CompiledEdge
					selectErr     error
				)

				if c.journal != nil && runID != "" {
					selectedEdges, selectErr = compiledDef.SelectOutgoingDurable(
						execCtx, c.journal, runID, sourceNode, st,
					)
				} else {
					selectedEdges, selectErr = compiledDef.SelectOutgoing(
						sourceNode,
						func(condition string) (bool, error) {
							return EvaluateCondition(condition, st)
						},
					)
				}

				if selectErr != nil {
					return false, xerr.Internal(
						fmt.Sprintf("flow: evaluate branch from %q: %v", sourceNode, selectErr),
						selectErr,
					)
				}

				for _, selected := range selectedEdges {
					if selected.To == targetNode {
						return true, nil
					}
				}

				return false, nil
			}).Build()

			dagBuilder.AddNode(gateID, gateOutKey, gateAction)
			dagBuilder.AddEdge(edge.From, gateID)
			dagBuilder.AddEdge(gateID, edge.To)

			incomingGates[edge.To] = append(incomingGates[edge.To], gateID)
		} else {
			dagBuilder.AddEdge(edge.From, edge.To)
		}
	}

	compiledDAG, compileErr := dagBuilder.Compile()
	if compileErr != nil {
		return nil, nil, xerr.Internal("flow: compile DAG topology", compileErr)
	}

	return compiledDAG, compiledDef, nil
}
