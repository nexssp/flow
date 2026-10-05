package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/nexssp/flow/cli/selftest"
)

func setupLogger() {
	level := slog.LevelInfo
	if env := os.Getenv("LOG_LEVEL"); env != "" {
		switch strings.ToLower(env) {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// commands defines the O(1) routing map for the CLI.
var commands = map[string]func(context.Context, *invocation, []string) int{
	"init":       func(_ context.Context, _ *invocation, args []string) int { return runInit(args) },
	"run":        runFlowInInvocation,
	"build":      runBuildInInvocation,
	"serve":      runServe,
	"lint":       runLintInInvocation,
	"explain":    runExplain,
	"expand":     runExpand,
	"catalog":    runCatalog,
	"list":       runList,
	"show":       runShow,
	"completion": func(_ context.Context, _ *invocation, args []string) int { return runCompletion(args) },
}

// Run is the central CLI dispatcher.
func Run(args []string) int {
	return RunContext(context.Background(), args)
}

// RunContext is Run with a caller-controlled parent context. It also cancels
// the invocation on Ctrl+C/SIGINT and, where supported, SIGTERM.
func RunContext(ctx context.Context, args []string) int {
	inv, err := newInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runCLIWithSignals(ctx, inv, args, registerProcessSignals)
}

func runCLI(ctx context.Context, inv *invocation, args []string) int {
	setupLogger()

	if len(args) == 0 {
		PrintTopHelp(os.Stdout)
		return 2
	}

	cmd := args[0]

	if cmd == flagHelpWord || cmd == flagHelp || (cmd == flagHelpShort && len(args) == 1) {
		return handleHelp(args[1:])
	}

	if (cmd == "-v" && len(args) == 1) || cmd == "version" || cmd == "--version" {
		Print(os.Stdout)
		return 0
	}

	rest := args[1:]

	if wantsHelp(args) {
		PrintCommandHelp(os.Stdout, cmd)
		return 0
	}

	// Handle special cases not in the function map
	if cmd == "self" {
		return runSelf(ctx, inv, rest)
	}
	if cmd == "require" {
		return handleRequire(ctx, inv, rest)
	}
	if cmd == "pin" {
		return runRequirePin(ctx, rest)
	}
	if cmd == "info" {
		return runFlowInInvocation(ctx, inv, append([]string{"--info"}, rest...))
	}

	if fn, ok := commands[cmd]; ok {
		return fn(ctx, inv, rest)
	}

	// Fallback to inline DSL or file execution
	return runFlowInInvocation(ctx, inv, args)
}

func handleHelp(args []string) int {
	if len(args) == 0 {
		PrintTopHelp(os.Stdout)
		return 0
	}
	if !PrintCommandHelp(os.Stdout, args[0]) {
		fmt.Fprintf(os.Stderr, "nflow: unknown command %q\n", args[0])
		return 2
	}
	return 0
}

func runSelf(ctx context.Context, inv *invocation, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nflow self <up|test> [flags] [filter...]")
		return 2
	}
	switch args[0] {
	case "up", "update":
		if err := SelfBuild(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "self-build error: %v\n", err)
			return 1
		}
		return 0
	case "test":
		flags := args[1:]
		bundles, err := inv.selftestBundles()
		if err != nil {
			fmt.Fprintf(os.Stderr, "self test: bundles: %v\n", err)
			return 1
		}
		return selftest.RunWithHost(ctx, selftest.Options{
			Out:          os.Stdout,
			Filters:      parseFilters(flags),
			Verbose:      hasFlag(flags, "--verbose") || hasFlag(flags, "-v"),
			NoColor:      hasFlag(flags, "--no-color"),
			JSON:         hasFlag(flags, "--json"),
			SaveBaseline: hasFlag(flags, "--save-baseline"),
		}, bundles, inv.host)
	default:
		fmt.Fprintf(os.Stderr, "unknown self command: %s\n", args[0])
		return 2
	}
}

func parseFilters(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		out = append(out, a)
	}
	return out
}

func hasFlag(args []string, name string) bool {
	return slices.Contains(args, name)
}

func runCompletion(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nflow completion <bash|zsh|pwsh>")
		return 2
	}
	if err := Completion(args[0], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "completion error: %v\n", err)
		return 1
	}
	return 0
}

func handleRequire(ctx context.Context, inv *invocation, args []string) int {
	if len(args) > 0 && args[0] == "pin" {
		return runRequirePin(ctx, args[1:])
	}
	return runRequire(ctx, inv, args)
}

func fatalf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
	return 2
}
