package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/nexssp/flow/builder"
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

const (
	versionCommand    = "version"
	buildCommand      = "build"
	serveCommand      = "serve"
	explainCommand    = "explain"
	expandCommand     = "expand"
	catalogCommand    = "catalog"
	completionCommand = "completion"
	requireCommand    = "require"
	toolchainCommand  = "toolchain"
)

var commands = map[string]func(context.Context, *invocation, []string) int{
	"init": func(_ context.Context, _ *invocation, args []string) int { return runInit(args) },
	"run":  runFlowInInvocation,
	buildCommand: func(ctx context.Context, inv *invocation, args []string) int {
		return builder.RunBuild(ctx, args, inv.sourceRequiresFromFile)
	},
	serveCommand:      runServe,
	"lsp":             runLSP,
	"lint":            runLintInInvocation,
	explainCommand:    runExplain,
	"fmt":             runFmt,
	expandCommand:     runExpand,
	catalogCommand:    runCatalog,
	"list":            runList,
	"show":            runShow,
	completionCommand: func(_ context.Context, _ *invocation, args []string) int { return runCompletion(args) },
}

func Run(args []string) int {
	return RunContext(context.Background(), args)
}

func RunContext(ctx context.Context, args []string) int {
	inv, err := newNativeInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	inv.toolchainEnabled = true
	return runCLIWithSignals(ctx, inv, args, registerProcessSignals)
}

func runCLI(ctx context.Context, inv *invocation, args []string) int {
	ctx = builder.WithGoDiagnostics(ctx)
	setupLogger()
	if inv != nil && inv.toolchainEnabled {
		if code, handled := maybeLaunchToolchain(ctx, inv, args); handled {
			return code
		}
		cleanArgs, _, err := stripNFlowVersionFlag(args)
		if err != nil {
			return fatalf("%v", err)
		}
		args = cleanArgs
	}

	if len(args) == 0 {
		PrintTopHelp(os.Stdout)
		return 2
	}

	cmd := args[0]

	if cmd == flagHelpWord || cmd == flagHelp || (cmd == flagHelpShort && len(args) == 1) {
		return handleHelp(args[1:])
	}

	if cmd == versionCommand {
		return runVersion(ctx, args[1:])
	}
	if (cmd == "-v" && len(args) == 1) || cmd == "--version" {
		Print(os.Stdout)
		return 0
	}

	rest := args[1:]

	if wantsHelp(args) {
		PrintCommandHelp(os.Stdout, cmd)
		return 0
	}

	if cmd == "self" {
		return runSelf(ctx, inv, rest)
	}
	if cmd == requireCommand {
		return handleRequire(ctx, inv, rest)
	}
	if cmd == "pin" {
		return runRequireSource(ctx, inv, requirePin, rest)
	}
	if cmd == "info" {
		return runFlowInInvocation(ctx, inv, append([]string{"--info"}, rest...))
	}

	if fn, ok := commands[cmd]; ok {
		return fn(ctx, inv, rest)
	}

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
		fmt.Fprintln(os.Stderr, "usage: nflow self <up|test|bench> [flags] [filter...]")
		return 2
	}
	switch args[0] {
	case "up", "update":
		if err := SelfBuild(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "self-build error: %v\n", err)
			return 1
		}
		return 0
	case "bench":
		return runSelfBench(ctx, inv, args[1:])
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
			SyntaxOnly:   hasFlag(flags, "--syntax-only"),
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
	if len(args) > 0 {
		switch args[0] {
		case requireCheck, requireUpdate, requirePin, requireReset:
			return runRequireSource(ctx, inv, args[0], args[1:])
		case "freeze":
			return fatalf("require freeze was removed; use require pin to write canonical versions into the .nflow source")
		}
	}
	return runRequire(ctx, inv, args)
}

func fatalf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
	return 2
}
