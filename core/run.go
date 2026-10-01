package core

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type RunReq struct {
	Program action.AnyAction
	Payload any
}

type RunRes struct {
	Output any
}

type CompileAndRunReq struct {
	Source  string
	Payload any
}

func RunAction() *action.BuiltAction[RunReq, RunRes] {
	return action.New("run", func(ctx context.Context, req RunReq) (RunRes, error) {
		if req.Program == nil {
			return RunRes{}, xerr.BadRequest("run: program is nil")
		}
		out, err := action.InvokeAny(ctx, req.Program, req.Payload)
		if err != nil {
			return RunRes{}, err
		}
		return RunRes{Output: out}, nil
	}).
		Description("Execute a compiled program").
		Tag("core", "runner").
		Build()
}

func CompileAndRun(
	resolver CapabilityResolver,
	dt *DirectiveTable,
	mt *ModifierTable,
	ot *OperatorTable,
	pt *PrimaryExtensionTable,
	_ ...CompileOption,
) *action.BuiltAction[CompileAndRunReq, RunRes] {
	compile := CompileAction(resolver, dt, mt, ot, pt)
	run := RunAction()

	return action.New("compile_and_run", func(ctx context.Context, req CompileAndRunReq) (RunRes, error) {
		cres, err := compile.Do(ctx, CompileReq{Source: req.Source})
		if err != nil {
			return RunRes{}, err
		}
		return run.Do(ctx, RunReq{Program: cres.Program, Payload: req.Payload})
	}).
		Description("Compile .nflow source and immediately execute it").
		Tag("core").
		Build()
}
