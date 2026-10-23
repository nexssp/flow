package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nexssp/flow/cli/selfbench"
	"github.com/nexssp/flow/runner"
)

type selfBenchArgs struct {
	path    string
	filters []string
	options selfbench.Options
}

func parseSelfBenchArgs(args []string, errorOutput io.Writer) (selfBenchArgs, error) {
	var parsed selfBenchArgs
	flags := flag.NewFlagSet("self bench", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	flags.StringVar(&parsed.path, "path", "", "scenario .nflow file or directory (replaces built-in suite)")
	flags.IntVar(&parsed.options.Iterations, "iterations", 50, "warm measured iterations (1..1000000)")
	flags.IntVar(&parsed.options.Warmup, "warmup", 3, "unmeasured warmups after first run (0..1000000)")
	flags.BoolVar(&parsed.options.JSON, "json", false, "JSON report")
	flags.BoolVar(&parsed.options.List, "list", false, "list matching scenarios without running")
	// Standard flag parsing stops at the first positional argument. Separate
	// filters while retaining flag/value pairs, so options may follow filters.
	var options []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			parsed.filters = append(parsed.filters, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			parsed.filters = append(parsed.filters, arg)
			continue
		}
		options = append(options, arg)
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if (name == "path" || name == "iterations" || name == "warmup") && !strings.Contains(arg, "=") {
			if i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		}
	}
	if err := flags.Parse(options); err != nil {
		return parsed, err
	}
	if parsed.options.Iterations < 1 || parsed.options.Iterations > 1_000_000 {
		return parsed, errors.New("iterations must be 1..1000000")
	}
	if parsed.options.Warmup < 0 || parsed.options.Warmup > 1_000_000 {
		return parsed, errors.New("warmup must be 0..1000000")
	}
	if len(flags.Args()) > 0 {
		return parsed, fmt.Errorf("unexpected option values: %s", strings.Join(flags.Args(), " "))
	}
	return parsed, nil
}

func runSelfBench(ctx context.Context, inv *invocation, args []string) int {
	parsed, err := parseSelfBenchArgs(args, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "self bench: %v\n", err)
		return 2
	}
	scenarios, err := selfbench.Discover(parsed.path, parsed.filters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	parsed.options.Out = os.Stdout
	if parsed.options.List {
		return selfbench.Run(ctx, runner.Config{}, scenarios, parsed.options)
	}
	cfg, err := inv.buildConfig(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "self bench: config: %v\n", err)
		return 1
	}
	// Custom flows can write to stdout. Keep their output on stderr in JSON
	// mode; the report writer retains the original stdout. CLI dispatch is serial.
	if parsed.options.JSON {
		stdout := os.Stdout
		os.Stdout = os.Stderr
		defer func() { os.Stdout = stdout }()
	}
	return selfbench.Run(ctx, cfg, scenarios, parsed.options)
}
