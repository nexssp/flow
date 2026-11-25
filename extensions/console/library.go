// Package console contains generic output actions for NFlow.
package console

import (
	"context"
	"embed"
	"fmt"
	"io"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/compiler"
)

const ID = "console"

//go:embed nflows
var fixtures embed.FS

// StderrRequest is the typed request for console.stderr.
type StderrRequest struct {
	Message string `json:"message"`
}

type synchronizedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

var writerKey = xctx.NewKey[*synchronizedWriter]("flow.console.stderr")

// WithStderrWriter injects the writer used by console.stderr. A nil writer
// leaves the context unchanged; the action then returns a configuration error.
func WithStderrWriter(ctx context.Context, writer io.Writer) context.Context {
	if ctx == nil || writer == nil {
		return ctx
	}
	return writerKey.With(ctx, &synchronizedWriter{w: writer})
}

func init() {
	compiler.Register(ID, Bundle)
}

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{ID: ID, Libraries: []action.Library{Library()}, Fixtures: fixtures}
}

func Library() action.Library {
	stderr := action.New("console.stderr", func(ctx context.Context, req StderrRequest) (any, error) {
		writer, ok := writerKey.From(ctx)
		if !ok || writer == nil || writer.w == nil {
			return nil, xerr.Unavailable("console.stderr: no stderr writer is injected")
		}
		writer.mu.Lock()
		defer writer.mu.Unlock()
		if _, err := fmt.Fprintln(writer.w, req.Message); err != nil {
			return nil, xerr.Unavailable("console.stderr: write", err)
		}
		return nil, nil //nolint:nilnil // A sink has no output value.
	}).Description("Write one message to the runner's injected stderr writer").Build()

	return action.Library{Name: ID, Actions: []action.AnyAction{stderr}}
}
