// Package flow — compiler.go owns the Compiler type and its top-level
// Compile method. Supporting code lives in sibling files:
//
//	compiler_ctx.go     context keys and helpers used by Compile
//	compiler_hooks.go   profile hook resolution
//	compiler_resolve.go capability lookup and payload unpacking
//
// The split keeps this file to the compilation pipeline itself, so the
// running commentary on Compile stays readable even as the rest of the
// package grows.
package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/nexssp/cost"
	kernelcost "github.com/nexssp/cost/adapters/kernel"
	"github.com/nexssp/flow/contracts"
	"github.com/nexssp/flow/journal"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
)

// ApprovalGate is the interface the compiler uses to ask for human
// approval before running a node marked as high-risk. It matches
// runner.TerminalApprovalGate and the mock gate used in tests.
type ApprovalGate interface {
	Check(ctx context.Context, actionName, argsJSON, token string) error
}

// Compiler turns a GraphDefinition into a runnable dag.DAG.
//
// A Compiler is safe for concurrent use by multiple goroutines: every
// field is read-only after NewCompiler returns, and per-call state
// lives in the ctx and in the returned CompiledGraph.
type Compiler struct {
	registry *action.Registry
	journal  journal.BranchJournal
	gate     ApprovalGate
	reserver cost.Reserver
	hooks    []action.AnyHook
	secObs   SecurityObserver
}

// NewCompiler builds a Compiler with sensible defaults: an in-memory
// branch journal, no approval gate, no cost reserver, no extra hooks,
// no security observer. Every default can be overridden with an option.
func NewCompiler(reg *action.Registry, opts ...func(*Compiler)) *Compiler {
	c := &Compiler{
		registry: reg,
		journal:  journal.NewMemoryBranchJournal(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithJournal installs a branch journal. A nil argument is ignored so
// callers can pass an optionally-nil dependency without a guard.
func WithJournal(j journal.BranchJournal) func(*Compiler) {
	return func(c *Compiler) {
		if j != nil {
			c.journal = j
		}
	}
}

// WithApprovalGate installs the approval gate. A nil argument is
// ignored: a graph that declares approval requirements but has no gate
// fails at compile time with a clear message, not silently here.
func WithApprovalGate(g ApprovalGate) func(*Compiler) {
	return func(c *Compiler) {
		if g != nil {
			c.gate = g
		}
	}
}

// WithReserver installs the cost reserver. Nil is a valid value and
// disables per-node budget reservation.
func WithReserver(r cost.Reserver) func(*Compiler) {
	return func(c *Compiler) {
		c.reserver = r
	}
}

// WithHooks appends hooks that run on every node in the graph. They
// are applied after the profile hooks, so a caller-provided hook can
// observe what a profile hook decided.
func WithHooks(hooks ...action.AnyHook) func(*Compiler) {
	return func(c *Compiler) {
		c.hooks = append(c.hooks, hooks...)
	}
}

// WithSecurityObserver installs an observer for profile hook events.
// Nil is ignored.
func WithSecurityObserver(o SecurityObserver) func(*Compiler) {
	return func(c *Compiler) {
		if o != nil {
			c.secObs = o
		}
	}
}

// CompilePipeline compiles a single expression string against the
// compiler's registry. Used by nodes that need to compile a child
// pipeline at run time (supervisor, dispatch fallback).
func (c *Compiler) CompilePipeline(expr string) (action.Executable, error) {
	if c == nil || c.registry == nil {
		return nil, fmt.Errorf("flow: nil compiler or registry")
	}
	builder, err := CompilePipeline(expr, c.registry)
	if err != nil {
		return nil, err
	}
	return builder.Build(), nil
}

// Compile turns a GraphDefinition into a runnable DAG.
//
// The pipeline is:
//
//  1. Merge @gate rules from the compilation context into the policy
//     (applyGates). This must happen before Compile(def) freezes the
//     definition into CompiledGraph.Definition.
//  2. Compile the definition into a CompiledGraph (topological
//     sorting, edge validation, cycle detection).
//  3. Resolve the profile and its hooks.
//  4. Enforce the capability allowlist per node.
//  5. Enforce "approval required but no gate configured".
//  6. For every node, build a dag action that:
//     - respects the per-node concurrency limiter,
//     - honours incoming gate outputs,
//     - assembles the request from Params, InputBindings, and upstream
//     node outputs,
//     - enforces MaxContextBytes,
//     - checks the approval gate when required,
//     - places the registry and compiler into the execution context,
//     - decodes the request into the action's typed shape.
//  7. Apply timeout, retry, cost reservation, profile hooks, and
//     compiler-wide hooks in that order.
//  8. For every conditional edge, insert a gate action that picks the
//     outgoing edges using the journal (durable) or the evaluator.
//
// The returned CompiledGraph is what callers inspect for topology,
// layers, and per-node metadata.
func (c *Compiler) Compile(ctx context.Context, def GraphDefinition) (*dag.DAG, *CompiledGraph, error) {
	// @gate rules from the compilation context must reach def before
	// Compile(def) snapshots the definition into CompiledGraph.Definition.
	applyGates(ctx, &def)

	compiledDef, err := Compile(def)
	if err != nil {
		return nil, nil, err
	}

	profilePolicy, err := LookupProfile(string(def.Profile))
	if err != nil {
		return nil, nil, xerr.BadRequest(err.Error())
	}

	profileHooks, err := c.resolveProfileHooks(profilePolicy)
	if err != nil {
		return nil, nil, err
	}

	for i := range def.Nodes {
		nodeSpec := def.Nodes[i]
		if !profilePolicy.Allows(nodeSpec.Capability) {
			return nil, nil, xerr.Forbidden(fmt.Sprintf(
				"flow: node %q capability %q is not permitted by profile %q",
				nodeSpec.ID, nodeSpec.Capability, def.Profile,
			))
		}
	}

	// hasApprovalRequirement is true only when some node actually
	// requires a gate. A policy that lists effect classes but has no
	// node carrying that effect is not an error: the user may have
	// declared the rule ahead of the nodes that will use it.
	hasApprovalRequirement := false
	for i := range def.Nodes {
		node := &def.Nodes[i]
		if node.Approval {
			hasApprovalRequirement = true
			break
		}
		if slices.Contains(def.Policy.ApprovalRequiredFor, node.Effect) {
			hasApprovalRequirement = true
			break
		}
	}

	if hasApprovalRequirement && c.gate == nil {
		return nil, nil, xerr.BadRequest(fmt.Sprintf(
			"flow: graph %q defines human approval requirements but no "+
				"ApprovalGate was configured on Compiler",
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

			// Skip the node if any incoming gate said "no".
			for _, gateID := range incomingGates[nodeSpec.ID] {
				if passed, found := dag.GetNodeOutput[bool](nCtx.Input, gateID); found == nil && !passed {
					return nil, nil
				}
			}

			payloadMap := make(map[string]any,
				len(nodeSpec.Params)+len(nodeSpec.InputBindings)+16)
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
							unpackIntoMap(payloadMap, directVal)
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

				payloadData, mErr = json.Marshal(payloadMap)
				if mErr != nil {
					return nil, xerr.Internal("flow: marshal node payload", mErr)
				}

				if def.Policy.MaxContextBytes > 0 &&
					int64(len(payloadData)) > def.Policy.MaxContextBytes {
					return nil, xerr.BadRequest(fmt.Sprintf(
						"flow: node %q payload (%d bytes) exceeds max_context_bytes (%d)",
						nodeSpec.ID, len(payloadData), def.Policy.MaxContextBytes,
					))
				}
			}

			requiresApproval := nodeSpec.Approval ||
				slices.Contains(def.Policy.ApprovalRequiredFor, nodeSpec.Effect)
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

			execCtx = contracts.WithRegistry(execCtx, c.registry)
			execCtx = contracts.WithCompiler(execCtx, c)

			return act.ExecuteDecoded(execCtx, func(target any) error {
				if len(payloadData) == 0 {
					// Seed *any with an empty map so @{ ... } injection has a
					// writable target. Without this, a node whose request type is
					// `any` receives nil, injection silently bails, and downstream
					// actions like `const` fail with a misleading error.
					if anyPtr, ok := target.(*any); ok {
						*anyPtr = map[string]any{}
					}
					return nil
				}
				if uErr := json.Unmarshal(payloadData, target); uErr != nil {
					return xerr.Internal("flow: unmarshal node payload", uErr)
				}
				return nil
			})
		})

		if nodeSpec.TimeoutMS > 0 {
			dagAction.Timeout(time.Duration(nodeSpec.TimeoutMS) * time.Millisecond)
		}

		maxAttempts := nodeSpec.Retry.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = profilePolicy.MaxRetries
		}

		if maxAttempts > 0 {
			dagAction.Retry(
				maxAttempts,
				action.ExponentialJitter(100*time.Millisecond, 2*time.Second),
			)
		}

		if c.reserver != nil {
			estimate := nodeSpec.EstimateMicros
			if estimate < 0 {
				estimate = 0
			}
			dagAction.AnyHook(kernelcost.GuardAction(c.reserver, estimate))
		}

		if len(profileHooks) > 0 {
			dagAction.AnyHook(profileHooks...)
		}

		if len(c.hooks) > 0 {
			dagAction.AnyHook(c.hooks...)
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
				runID := xctx.ExecutionIDFrom(execCtx)
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
