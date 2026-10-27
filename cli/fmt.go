package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nexssp/kernel/xfs"

	flowformatter "github.com/nexssp/flow/formatter"
)

type fmtMode uint8

const (
	fmtToStdout fmtMode = iota
	fmtWrite
	fmtCheck
)

type fmtOptions struct {
	writeMode bool
	checkMode bool
	stdinMode bool
	files     []string
}

func runFmt(ctx context.Context, inv *invocation, args []string) int {
	if wantsHelp(args) {
		PrintCommandHelp(os.Stdout, "fmt")
		return 0
	}
	options, parseErr := parseFmtOptions(args)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "nflow fmt: %v\n", parseErr)
		return 2
	}
	if validationErr := validateFmtOptions(options); validationErr != nil {
		fmt.Fprintf(os.Stderr, "nflow fmt: %v\n", validationErr)
		return 2
	}
	if isFmtStdin(options) {
		return formatStdin(ctx, inv)
	}
	return formatFiles(ctx, inv, options.files, options.mode())
}

func parseFmtOptions(args []string) (fmtOptions, error) {
	var options fmtOptions
	for _, arg := range args {
		switch arg {
		case "--write":
			options.writeMode = true
		case "--check":
			options.checkMode = true
		case "--stdin":
			options.stdinMode = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return fmtOptions{}, fmt.Errorf("unknown flag %q", arg)
			}
			options.files = append(options.files, arg)
		}
	}
	return options, nil
}

func validateFmtOptions(options fmtOptions) error {
	switch {
	case options.writeMode && options.checkMode:
		return errors.New("--write and --check cannot be used together")
	case options.stdinMode && len(options.files) != 0:
		return errors.New("--stdin cannot be combined with file arguments")
	case options.stdinMode && (options.writeMode || options.checkMode):
		return errors.New("--stdin writes formatted source to stdout; use it without --write or --check")
	case isFmtStdin(options) && (options.writeMode || options.checkMode):
		return errors.New("--write and --check require named files")
	case len(options.files) > 1 && !options.writeMode && !options.checkMode:
		return errors.New("multiple files require --write or --check")
	default:
		return nil
	}
}

func isFmtStdin(options fmtOptions) bool {
	return options.stdinMode || len(options.files) == 0 || len(options.files) == 1 && options.files[0] == "-"
}

func (options fmtOptions) mode() fmtMode {
	if options.writeMode {
		return fmtWrite
	}
	if options.checkMode {
		return fmtCheck
	}
	return fmtToStdout
}

func formatFiles(ctx context.Context, inv *invocation, files []string, mode fmtMode) int {
	code := 0
	for _, path := range files {
		if err := formatFile(ctx, inv, path, mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	return code
}

func formatFile(ctx context.Context, inv *invocation, path string, mode fmtMode) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("nflow fmt: %s: %w", path, err)
	}
	formatter, err := formatterFor(ctx, inv, string(data), path)
	if err != nil {
		return err
	}
	formatted, err := formatter.Format(ctx, string(data), path)
	if err != nil {
		return err
	}
	switch mode {
	case fmtWrite:
		if string(data) == formatted {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("nflow fmt: %s: %w", path, err)
		}
		if err := xfs.WriteFileAtomic(path, []byte(formatted), info.Mode().Perm()); err != nil {
			return fmt.Errorf("nflow fmt: %s: %w", path, err)
		}
	case fmtCheck:
		if string(data) != formatted {
			return fmt.Errorf("%s: not formatted", path)
		}
	case fmtToStdout:
		if _, err := os.Stdout.WriteString(formatted); err != nil {
			return fmt.Errorf("nflow fmt: stdout: %w", err)
		}
	}
	return nil
}

func formatStdin(ctx context.Context, inv *invocation) int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nflow fmt: stdin: %v\n", err)
		return 1
	}
	formatter, err := formatterFor(ctx, inv, string(data), "<stdin>")
	if err != nil {
		fmt.Fprintf(os.Stderr, "nflow fmt: %v\n", err)
		return 1
	}
	formatted, err := formatter.Format(ctx, string(data), "<stdin>")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := os.Stdout.WriteString(formatted); err != nil {
		fmt.Fprintf(os.Stderr, "nflow fmt: stdout: %v\n", err)
		return 1
	}
	return 0
}

func formatterFor(ctx context.Context, inv *invocation, source, name string) (*flowformatter.Formatter, error) {
	_, metadata, cfg, err := inv.prepareRequiredSource(ctx, source, name)
	if err != nil {
		return nil, err
	}
	_, requireEnabled := cfg.Directives.ByName("require")
	return flowformatter.NewWithParserTables(cfg.Operators, cfg.PrimariesFor(metadata), requireEnabled, cfg.Directives.All()...), nil
}
