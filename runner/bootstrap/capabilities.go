package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/flows"
	"github.com/nexssp/flow/runner/capability"
	"github.com/nexssp/kernel/action"
)

// WithActions is the universal escape hatch. Use it for anything that does
// not have a dedicated With* helper, including domain services you build
// by hand.
func (a *App) WithActions(fn func(context.Context) ([]action.AnyAction, error)) *App {
	a.loaders = append(a.loaders, func(asm *Assembly) error {
		acts, err := fn(context.Background())
		if err != nil {
			return err
		}

		asm.Actions = append(asm.Actions, acts...)

		return nil
	})

	return a
}

// WithFlows compiles every *.flow file in dir into a named action.
//
// Two passes:
//
//  1. Read all flow sources and install any :exec= / :remote= / :wasm=
//     capability bindings so their short names resolve when the compiler
//     reads the sources.
//  2. Compile each flow into a named action whose Name is the file stem.
//
// Load WithFlows last so its nodes see every other loader's actions.
func (a *App) WithFlows(dir string) *App {
	a.loaders = append(a.loaders, func(asm *Assembly) error {
		if _, err := os.Stat(dir); err != nil {
			return nil
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("flow dir %q: %w", dir, err)
		}

		type flowEntry struct {
			name   string
			path   string
			source string
		}

		var (
			flowsList []flowEntry
			combined  strings.Builder
		)

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".flow") {
				continue
			}

			path := filepath.Join(dir, e.Name())

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read flow %q: %w", path, err)
			}

			flowsList = append(flowsList, flowEntry{
				name:   strings.TrimSuffix(e.Name(), ".flow"),
				path:   path,
				source: string(data),
			})
			combined.Write(data)
			combined.WriteByte('\n')
		}

		baseReg, err := action.NewRegistry(action.Of(asm.Actions...))
		if err != nil {
			return fmt.Errorf("flow: build base registry: %w", err)
		}

		// Pass 1: resolve capability bindings. Failure to resolve any
		// single binding is non-fatal; the compiler surfaces a clear
		// "capability not found" error per flow when it tries to use it.
		resolver, rerr := capability.NewResolver(context.Background(), baseReg, combined.String())
		if rerr == nil {
			defer resolver.Close()

			if remoteActions := resolver.Actions(); len(remoteActions) > 0 {
				merged, mergeErr := action.NewRegistry(
					action.Library{Name: "base", Actions: baseReg.Actions()},
					action.Library{Name: "capabilities", Actions: remoteActions},
				)
				if mergeErr != nil {
					return fmt.Errorf("flow: merge capabilities: %w", mergeErr)
				}
				baseReg = merged
			}
		}

		// Pass 2: compile each flow into a named action.
		for _, f := range flowsList {
			act, cerr := compileFlowFile(f.name, f.source, baseReg)
			if cerr != nil {
				return fmt.Errorf("compile flow %q: %w", f.path, cerr)
			}

			asm.Actions = append(asm.Actions, act)
		}

		return nil
	})

	return a
}

// compileFlowFile reads a .flow source, sanitizes the DSL, and returns a
// single action whose Name is the file stem. The compiled graph is
// independent of the caller — the returned action executes the flow
// end-to-end with a JSON payload.
func compileFlowFile(name, source string, reg *action.Registry) (action.AnyAction, error) {
	dsl := flow.SanitizeDSL(source)
	if dsl == "" {
		return nil, fmt.Errorf("no executable pipeline")
	}
	compiler := flow.NewCompiler(reg)
	execAct := flow.NewExecuteAction(compiler)
	return action.New(name, func(ctx context.Context, payload map[string]any) (flow.GraphExecRes, error) {
		return execAct.Do(ctx, flow.GraphExecReq{
			DSL:            dsl,
			InitialPayload: payload,
		})
	}).Tag("flow").Build(), nil
}

// WithFlowsFolder scans a directory for .flow files that declare
// @action and registers each one as a callable action, plus three
// meta-tools (flow.list, flow.inspect, flow.run).
//
// Registration is deferred to the assembly phase, so the flows see the
// final action set produced by every other With* call.
func (a *App) WithFlowsFolder(root string) *App {
	a.loaders = append(a.loaders, func(asm *Assembly) error {
		defs, err := flows.ScanFlowsFolder(root)
		if err != nil {
			return fmt.Errorf("scan flows folder %q: %w", root, err)
		}

		if len(defs) == 0 {
			return nil
		}

		existing := make(map[string]bool, len(asm.Actions))
		for _, act := range asm.Actions {
			if meta := act.Describe(); meta != nil {
				existing[meta.Name] = true
			}
		}

		for _, d := range defs {
			if existing[d.Name] {
				return fmt.Errorf("flow: %s collides with an existing action", d.Name)
			}
		}

		flowActions := flows.BuildFlowActions(defs, func() []action.AnyAction {
			return asm.Actions
		})

		actionMap := make(map[string]action.AnyAction, len(flowActions))
		for _, act := range flowActions {
			actionMap[act.Describe().Name] = act
		}

		meta := flows.BuildFlowMetaTools(defs, actionMap)

		asm.Actions = append(asm.Actions, flowActions...)
		asm.Actions = append(asm.Actions, meta...)

		slog.Info("flows folder scanned", "root", root, "flows", len(defs))

		return nil
	})

	return a
}
