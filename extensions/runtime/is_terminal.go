package runtime

import (
	"context"
	"os"

	"github.com/nexssp/kernel/action"

	"github.com/nexssp/flow/internal/terminal"
)

// IsTerminal reports whether standard output is attached to an interactive terminal.
var IsTerminal = action.New("runtime.is_terminal", func(_ context.Context, _ any) (bool, error) {
	return terminal.IsTerminal(os.Stdout), nil
}).Description("Report whether stdout is connected to an interactive terminal").Tag("base", "runtime", "terminal").Build()
