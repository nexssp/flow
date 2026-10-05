package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/extensions/on"
)

func runServe(ctx context.Context, inv *invocation, args []string) int {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}
	return withHarness(ctx, inv, "serve", path, args, runServeInProcess)
}

func runServeInProcess(parent context.Context, inv *invocation, args []string) int {
	if len(args) < 1 {
		return fatalf("usage: nflow serve <file.nflow>")
	}

	ctx := parent

	path := args[0]
	src, err := os.ReadFile(path)
	if err != nil {
		return fatalf("read source: %v", err)
	}

	reqs, err := inv.sourceRequiresFromFile(ctx, path)
	if err != nil {
		return fatalf("requires: %v", err)
	}
	cfg, err := inv.buildConfig(reqs)
	if err != nil {
		return fatalf("build config: %v", err)
	}

	clean, meta, err := core.Preprocess(ctx, cfg.Directives, string(src), path)
	if err != nil {
		return fatalf("preprocess: %v", err)
	}

	trigger, ok := meta["on_event"].(on.EventTrigger)
	if !ok {
		return fatalf("file does not declare an '@on event' trigger")
	}

	if _, err := core.CompileAction(cfg.Resolver, cfg.Directives, cfg.Modifiers, cfg.Operators, cfg.Primaries).
		Do(ctx, core.CompileReq{Source: clean, Name: path}); err != nil {
		return fatalf("compile: %v", err)
	}

	fmt.Printf("serving %s over protocol %s on %s\n", path, trigger.Protocol, trigger.Target)

	<-ctx.Done()
	fmt.Println("shutting down listener")
	return 0
}
