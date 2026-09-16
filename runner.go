package flow

import (
	"context"
	"io"

	"github.com/nexssp/kernel/action"
)

// Runner is the shape of a flow execution engine.
//
// The default implementation is flow/runner.Default. Products that need
// a different engine — remote execution, a custom observer, a custom
// approval flow — implement this interface and swap it in.
//
// The interface is deliberately minimal: it captures only what callers
// actually need (flow path, initial payload, raw CLI args, libraries,
// and where to write output). The implementation owns the registry,
// the observer, the approval gate, and every other detail.
//
// Callers that only need the default runner can import
// github.com/nexssp/flow/runner directly and skip this interface.
type Runner interface {
	// RunFlow executes the flow file at path.
	//
	//   args    is the raw CLI flag slice; config knobs are parsed
	//           downstream by flow.ResolveConfig.
	//   libs    are the libraries whose actions the flow may call.
	//   stdout  receives the human-readable trace and metrics table.
	//   stderr  receives fatal errors and warnings.
	//
	// Returns a process exit code: 0 on success, non-zero otherwise.
	RunFlow(
		ctx context.Context,
		path string,
		payload map[string]any,
		args []string,
		libs []action.Library,
		stdout, stderr io.Writer,
	) int
}
