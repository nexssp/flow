package flow

import (
	"context"

	"github.com/nexssp/kernel/action"
)

type pipelineStreamAction struct {
	meta     *action.Meta
	source   action.AnyStreamAction
	ops      StreamOperator
	anyHooks []action.AnyHook
}

func (p *pipelineStreamAction) Describe() *action.Meta { return p.meta }
func (p *pipelineStreamAction) GetBindings() []action.Binding {
	return p.source.GetBindings()
}
func (p *pipelineStreamAction) ReqPayload() any { return p.source.ReqPayload() }
func (p *pipelineStreamAction) ResPayload() any { return p.source.ResPayload() }

func (p *pipelineStreamAction) GetAnyHooks() []action.AnyHook {
	return append([]action.AnyHook(nil), p.anyHooks...)
}

func (p *pipelineStreamAction) AddAnyHook(hooks ...action.AnyHook) {
	p.anyHooks = append(p.anyHooks, hooks...)
}

func (p *pipelineStreamAction) CloneWithHooks(hooks ...action.AnyHook) action.AnyStreamAction {
	clone := &pipelineStreamAction{
		meta:     p.meta,
		source:   p.source,
		ops:      p.ops,
		anyHooks: append([]action.AnyHook(nil), p.anyHooks...),
	}
	clone.anyHooks = append(clone.anyHooks, hooks...)
	return clone
}

func (p *pipelineStreamAction) DoStreamAny(ctx context.Context, request any) (action.AnyStream, error) {
	upstream, err := p.source.DoStreamAny(ctx, request)
	if err != nil {
		return nil, err
	}
	return p.ops.Apply(upstream)
}

type configuredStreamSource struct {
	action.AnyStreamAction
	compileConfig map[string]any
}

func (s *configuredStreamSource) DoStreamAny(ctx context.Context, request any) (action.AnyStream, error) {
	merged := make(map[string]any, len(s.compileConfig)+8)
	for key, value := range s.compileConfig {
		merged[key] = value
	}

	// Merge the request field by field. Never let the request replace
	// the whole map: action.Assign does that when the types match, and
	// a request that happens to be an empty map would then wipe out
	// every compile-time modifier the atom was compiled with.
	if reqMap, ok := request.(map[string]any); ok {
		for k, v := range reqMap {
			merged[k] = v
		}
	}

	return s.AnyStreamAction.DoStreamAny(ctx, merged)
}

type identityOperator struct{}

func (identityOperator) Name() string { return "identity" }
func (identityOperator) Apply(upstream action.AnyStream) (action.AnyStream, error) {
	return upstream, nil
}

// StreamDrainResult is the value produced by a stream pipeline that
// was drained to a single summary without a boundary.
//
// A pipeline like:
//
//	fs.walk -> fs.read
//
// (without a `collect` boundary and without any downstream unary
// node) does not return the individual items. Instead it runs the
// stream to completion and returns the number of items that were
// emitted. This is what a caller gets from `Do` when they compile a
// stream pipeline without a boundary.
//
// The type is exported so that external test packages and consumers
// can assert on the concrete value without relying on anonymous
// structs.
type StreamDrainResult struct {
	Count int `json:"count"`
}

// CountStreamResult is a convenience helper that extracts the item
// count from a value produced by a drained stream pipeline. It returns
// 0 for values that do not carry a Count field.
func CountStreamResult(res any) int {
	if r, ok := res.(StreamDrainResult); ok {
		return r.Count
	}
	return 0
}

func streamAsUnary(source action.AnyStreamAction) action.AnyAction {
	name := source.Describe().Name
	return action.New(name, func(ctx context.Context, request any) (StreamDrainResult, error) {
		stream, err := source.DoStreamAny(ctx, request)
		if err != nil {
			return StreamDrainResult{}, err
		}
		count := 0
		var firstError error
		stream(func(_ any, itemError error) bool {
			if itemError != nil {
				firstError = itemError
				return false
			}
			count++
			return true
		})
		return StreamDrainResult{Count: count}, firstError
	}).Build()
}
