// Package progress ships a TTY progress reporter and two actions that
// publish progress events through kernel/xctx.
//
// The reporter is installed once per pipeline via WrapPipeline. Any
// action can publish xctx.ReportProgress; the reporter renders it.
// When stderr is a TTY, long-running steps show an animated spinner
// with elapsed time. When stderr is redirected, one line per completed
// step is written so CI logs stay readable.
//
// Typical use:
//
//	@require progress
//
//	{ duration_ms: 2000 }
//	-> progress.wrap @{ action: runtime.sleep, message: "Slow op" }
//	-> progress.step @{ message: "Ready" }
package progress

import (
	"context"
	"embed"
	"os"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"

	"github.com/nexssp/flow/core"
)

const ID = "progress"

//go:embed nflows
var fixturesFS embed.FS

// installedKey marks a context as already carrying a progress reporter.
// WrapPipeline uses it to avoid stacking reporters when a pipeline
// invokes a materialized sub-pipeline whose own WrapPipeline would
// otherwise install a second reporter over the first.
var installedKey = xctx.NewKey[bool]("flow.progress.installed")

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID:        ID,
		Libraries: []action.Library{Library()},
		ArgSchemas: map[string][]core.ArgFieldSpec{
			"progress.wrap": {{Name: "action", Kind: core.ArgCapabilityRef}},
		},
		WrapPipeline: wrapPipelineWithReporter,
		Fixtures:     fixturesFS,
	}
}

func Library() action.Library {
	return action.Library{
		Name: ID,
		Actions: []action.AnyAction{
			StepAction(),
			WrapAction(),
		},
	}
}

// wrapPipelineWithReporter installs a Reporter for the duration of the
// pipeline invocation. Nested pipelines see the flag and pass through.
func wrapPipelineWithReporter(_ map[string]any, inner action.AnyAction) (action.AnyAction, error) {
	return action.New("progress.pipeline", func(ctx context.Context, req any) (any, error) {
		if installed, _ := installedKey.From(ctx); installed {
			return action.InvokeAny(ctx, inner, req)
		}

		reporter := NewReporter(os.Stderr)
		defer reporter.Shutdown()

		ctx = installedKey.With(ctx, true)
		ctx = xctx.WithProgressReporter(ctx, reporter.Report)
		return action.InvokeAny(ctx, inner, req)
	}).Build(), nil
}
