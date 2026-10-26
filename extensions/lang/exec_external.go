package lang

import (
	"context"

	"github.com/nexssp/flow/extensions/external"
)

// externalExecAdapter delegates to extensions/external.Exec, the project's
// canonical subprocess action. This is the production executor.
//
// external.Exec is *action.BuiltAction[map[string]any, external.ExecResult].
// We call its Do(ctx, req) method directly — no type assertion needed, the
// concrete type is known at compile time. If the kernel ever changes the
// BuiltAction API, this fails to compile rather than silently degrading.
type externalExecAdapter struct{}

func (externalExecAdapter) Run(ctx context.Context, cmd string, args []string, stdin []byte) (stdoutText, stderrText string, runErr error) {
	// Pass argv directly so paths and flags survive Windows cmd.exe and POSIX
	// shells unchanged. external.Exec owns the process execution contract.
	in := map[string]any{"cmd": cmd, "args": args}
	if len(stdin) > 0 {
		in["input"] = stdin
	}

	res, execErr := external.Exec.Do(ctx, in)
	return res.Stdout, res.Stderr, execErr
}
