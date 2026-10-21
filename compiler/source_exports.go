package compiler

import (
	"context"

	"github.com/nexssp/kernel/action"
)

// QualifySourceLibrary prefixes complete capability names. Unlike @require's
// library alias, a source namespace never replaces an existing name component.
// bind restores the module's lexical execution context at exported boundaries.
func QualifySourceLibrary(lib action.Library, namespace string, bind func(context.Context) context.Context) action.Library {
	out := action.Library{Name: lib.Name}
	for _, raw := range lib.Actions {
		builder := action.Dynamic(raw).Name(namespace + "." + raw.Describe().Name)
		if bind != nil {
			builder = builder.UseFirst(func(next action.Fn[any, any]) action.Fn[any, any] {
				return func(ctx context.Context, in any) (any, error) {
					return next(bind(ctx), in)
				}
			})
		}
		out.Actions = append(out.Actions, builder.Build())
	}
	for _, raw := range lib.Sources {
		var source action.AnyStreamAction = &qualifiedStream{inner: raw, name: namespace + "." + raw.Describe().Name}
		if bind != nil {
			source = &sourceContextStream{AnyStreamAction: source, bind: bind}
		}
		out.Sources = append(out.Sources, source)
	}
	for _, raw := range lib.Operators {
		op := raw.Clone()
		name := namespace + "." + op.Name
		build := op.Build
		op.Name = name
		op.Build = func(params any) (action.StreamOperator, error) {
			inner, err := build(params)
			if err != nil {
				return nil, err
			}
			return qualifiedOperator{inner: inner, name: name}, nil
		}
		out.Operators = append(out.Operators, op)
	}
	return out
}

type sourceContextStream struct {
	action.AnyStreamAction
	bind func(context.Context) context.Context
}

func (s *sourceContextStream) DoStreamAny(ctx context.Context, req any) (action.AnyStream, error) {
	return s.AnyStreamAction.DoStreamAny(s.bind(ctx), req)
}

func (s *sourceContextStream) CloneWithHooks(hooks ...action.AnyHook) action.AnyStreamAction {
	return &sourceContextStream{AnyStreamAction: s.AnyStreamAction.CloneWithHooks(hooks...), bind: s.bind}
}
