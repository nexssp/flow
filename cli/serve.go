package cli

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/on"
)

// execCapabilities lists the atoms that execute arbitrary host code and
// therefore require --allow-exec when run under `nflow serve`. They are
// safe under `nflow run` (the operator chose the flow file) but not in a
// long-running process that may accept flows from other tenants.
var execCapabilities = []string{
	"external.exec",
	"external.wasm",
}

func runServe(ctx context.Context, inv *invocation, args []string) int {
	path := ""
	allowExec := false

	for _, arg := range args {
		switch {
		case arg == "--allow-exec":
			allowExec = true
		case path == "" && arg != "" && arg[0] != '-':
			path = arg
		}
	}
	return withHarness(ctx, inv, "serve", path, args, func(ctx context.Context, inv *invocation, _ []string) int {
		return runServeInProcess(ctx, inv, path, allowExec)
	})
}

func runServeInProcess(parent context.Context, inv *invocation, path string, allowExec bool) int {
	if path == "" {
		return fatalf("usage: nflow serve <file.nflow> [--allow-exec]")
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read source: %v", err)
	}

	reqs, err := inv.sourceRequiresFromFile(parent, path)
	if err != nil {
		return fatalf("requires: %v", err)
	}
	cfg, err := inv.buildConfig(reqs)
	if err != nil {
		return fatalf("build config: %v", err)
	}

	clean, meta, err := core.Preprocess(parent, cfg.Directives, string(src), path)
	if err != nil {
		return fatalf("preprocess: %v", err)
	}

	trigger, ok := meta["on_event"].(on.EventTrigger)
	if !ok {
		return fatalf("file does not declare an '@on event' trigger")
	}

	compiled, err := core.CompileAction(
		cfg.Resolver, cfg.Directives, cfg.Modifiers, cfg.Operators, cfg.Primaries,
	).Do(parent, core.CompileReq{Source: clean, Name: path})
	if err != nil {
		return fatalf("compile: %v", err)
	}

	if !allowExec {
		if name, found := firstExecCapability(compiled.AST); found {
			return fatalf(
				"serve: flow uses %q which runs host commands; "+
					"pass --allow-exec to acknowledge that serve will execute them", name)
		}
	}

	fmt.Fprintf(os.Stderr, "serving %s over protocol %s on %s\n",
		path, trigger.Protocol, trigger.Target)
	fmt.Fprintln(os.Stderr,
		"note: the listener is not implemented in this release; serve currently "+
			"only validates that the flow compiles and waits for shutdown. "+
			"Do not rely on it as a production service.")
	fmt.Fprintln(os.Stderr, "press Ctrl+C to exit")

	<-parent.Done()
	fmt.Fprintln(os.Stderr, "shutting down")
	return 0
}

func firstExecCapability(ast core.Expr) (string, bool) {
	if ast == nil {
		return "", false
	}
	for _, name := range core.AtomNames(ast) {
		if slices.Contains(execCapabilities, name) {
			return name, true
		}
	}
	return "", false
}
