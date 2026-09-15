package runner

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/nexssp/flow"
)

// Default is the standard flow.Runner implementation. It builds a
// registry from the supplied libraries, wires the observer, and
// delegates to RunWithRegistry.
//
// The zero value is safe to use.
type Default struct {
	// Observer is optional. When nil, a fresh observer is created
	// with verbosity resolved from the flow config.
	Observer *RunnerObserver
}

var _ flow.Runner = Default{}

func (d Default) RunFlow(
	ctx context.Context,
	path string,
	payload map[string]any,
	args []string,
	libs []flow.Library,
	stdout, stderr io.Writer,
) int {
	reg, err := flow.BuildRegistry(libs...)
	if err != nil {
		fmt.Fprintf(stderr, "❌ registry build failed: %v\n", err)

		return 1
	}

	obs := d.Observer
	if obs == nil {
		obs = NewRunnerObserver(stdout, 0)
	}

	return RunWithRegistry(ctx, Request{
		Path:    path,
		Payload: payload,
		Args:    args,
		Stdout:  stdout,
		Stderr:  stderr,
	}, reg, obs)
}

func (d Default) RunWithRegistry(
	ctx context.Context,
	req Request,
	reg *flow.MapRegistry,
	obs *RunnerObserver,
) int {
	if obs == nil {
		stdout := req.Stdout
		if stdout == nil {
			stdout = os.Stdout
		}

		obs = NewRunnerObserver(stdout, 0)
	}

	return RunWithRegistry(ctx, req, reg, obs)
}
