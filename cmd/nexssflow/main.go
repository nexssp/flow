// nexssp/flow/cmd/nexssflow/main.go
//
// Command nexssflow runs flow files. It is the reference entry point
// for the flow module: any project that only needs the DSL can build
// on it or wrap it, without pulling in ai, jumalu, or ops.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nexssp/flow"
	"github.com/nexssp/flow/runner"
	"github.com/nexssp/kernel/action"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		os.Exit(runCmd(os.Args[2:]))
	case "help", "--help", "-h":
		usage()
	default:
		// Allow `nexssflow file.flow` without the "run" verb.
		os.Exit(runCmd(os.Args[1:]))
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `nexssflow — nexss flow runner

Usage:
  nexssflow run <path.flow> [json_payload] [flags]
  nexssflow <path.flow>     [json_payload] [flags]

Flags:
  -v | -vv | -vvv        verbosity
  --budget=<usd>         spend ceiling
  --max-tokens=<n>       LLM ceiling
  --approval=<mode>      danger | all | none
  --resume=<runID>       resume a failed run from its checkpoint
  --info                 describe the flow and exit
  --assert="<expr>"      testkit assertion
`)
}

func runCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "❌ usage: nexssflow <path.flow> [json_payload] [flags]")

		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		path    string
		payload = map[string]any{}
		flags   []string
	)

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "{"):
			if err := json.Unmarshal([]byte(arg), &payload); err != nil {
				fmt.Fprintf(os.Stderr, "❌ invalid JSON payload: %v\n", err)

				return 1
			}
		case !strings.HasPrefix(arg, "-") && path == "":
			path = arg
		default:
			flags = append(flags, arg)
		}
	}

	if path == "" {
		fmt.Fprintln(os.Stderr, "❌ usage: nexssflow <path.flow> [json_payload] [flags]")

		return 1
	}

	libs := []action.Library{flow.StandardLibrary()}

	return runner.Default{}.RunFlow(
		ctx, path, payload, flags, libs,
		os.Stdout, os.Stderr,
	)
}
