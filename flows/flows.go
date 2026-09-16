package flows

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
)

type FlowDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

func ScanFlowsFolder(root string) ([]FlowDef, error) {
	var defs []FlowDef

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() || !strings.HasSuffix(path, ".flow") {
			return nil
		}

		pre, perr := flow.Preprocess(path)
		if perr != nil {
			return fmt.Errorf("scan %s: %w", path, perr)
		}

		if pre.Action == nil || pre.Action.Name == "" {
			return nil
		}

		defs = append(defs, FlowDef{
			Name:        pre.Action.Name,
			Description: pre.Action.Description,
			Path:        path,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })

	return defs, nil
}

func BuildFlowActions(defs []FlowDef, parent func() []action.AnyAction) []action.AnyAction {
	out := make([]action.AnyAction, 0, len(defs))
	for _, def := range defs {
		out = append(out, buildFlowAction(def, parent))
	}

	return out
}

func buildFlowAction(def FlowDef, parent func() []action.AnyAction) action.AnyAction {
	return action.New(def.Name, func(ctx context.Context, req map[string]any) (any, error) {
		pre, err := flow.Preprocess(def.Path)
		if err != nil {
			return nil, xerr.Internal("flow: preprocess "+def.Path, err)
		}

		dsl := flow.SanitizeDSL(pre.DSL)

		localReg, err := action.NewRegistry(action.Of(parent()...))
		if err != nil {
			return nil, err
		}

		localReg, err = flow.RegisterPipelines(localReg, pre.Pipelines)
		if err != nil {
			return nil, err
		}

		compiler := flow.NewCompiler(localReg)
		execAct := flow.NewExecuteAction(compiler)

		res, err := execAct.Do(ctx, flow.GraphExecReq{
			DSL:            dsl,
			InitialPayload: req,
		})
		if err != nil {
			return nil, err
		}

		return res.Outputs, nil
	}).
		Description(def.Description).
		Tag("flow", "action").
		Build()
}

func BuildFlowMetaTools(defs []FlowDef, flowActions map[string]action.AnyAction) []action.AnyAction {
	byName := make(map[string]FlowDef, len(defs))
	for _, d := range defs {
		byName[d.Name] = d
	}

	listAct := action.New("flow.list", func(_ context.Context, _ struct{}) ([]FlowDef, error) {
		out := make([]FlowDef, 0, len(defs))
		out = append(out, defs...)

		return out, nil
	}).
		Description("Lists every flow registered as an action").
		Tag("flow", "meta").
		Build()

	inspectAct := action.New("flow.inspect", func(_ context.Context, req struct {
		Name string `json:"name" validate:"required"`
	}) (map[string]any, error) {
		def, ok := byName[req.Name]
		if !ok {
			return nil, xerr.NotFound("flow: no flow named " + req.Name)
		}

		pre, err := flow.Preprocess(def.Path)
		if err != nil {
			return nil, err
		}

		pipes := make([]string, 0, len(pre.Pipelines))
		for _, p := range pre.Pipelines {
			pipes = append(pipes, p.Name)
		}

		sort.Strings(pipes)

		return map[string]any{
			"name":        def.Name,
			"description": def.Description,
			"path":        def.Path,
			"pipelines":   pipes,
			"dsl":         flow.SanitizeDSL(pre.DSL),
		}, nil
	}).
		Description("Describes a registered flow: DSL, pipelines, path").
		Tag("flow", "meta").
		Build()

	runAct := action.New("flow.run", func(ctx context.Context, req struct {
		Name    string         `json:"name" validate:"required"`
		Payload map[string]any `json:"payload,omitempty"`
	}) (any, error) {
		act, ok := flowActions[req.Name]
		if !ok {
			return nil, xerr.NotFound("flow: no flow named " + req.Name)
		}

		return action.InvokeAny(ctx, act, req.Payload)
	}).
		Description("Runs a registered flow by name with a payload").
		Tag("flow", "meta").
		Build()

	return []action.AnyAction{listAct, inspectAct, runAct}
}
