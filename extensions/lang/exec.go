package lang

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// executor is the seam between this bundle and extensions/external.
//
// Returns strings, not []byte — external.Exec's ExecResult uses string
// fields, and converting string→[]byte copies. Working in string throughout
// avoids that copy on every subprocess call.
//
// Tests can swap execRunner (see directive_test.go) to assert behavior
// without spawning real subprocesses.
type executor interface {
	Run(ctx context.Context, cmd string, args []string, stdin []byte) (stdout, stderr string, err error)
}

// defaultExecutor is the standalone-test fallback. Production wiring in
// library.go swaps execRunner to externalExecAdapter (delegates to
// external.Exec). Kept here so the package compiles and tests without
// the full flow repo.
type defaultExecutor struct{}

func (defaultExecutor) Run(ctx context.Context, cmd string, args []string, stdin []byte) (stdoutText, stderrText string, runErr error) {
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, cmd, args...)
	c.Stdin = bytes.NewReader(stdin)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

// execRunner is the package-level executor. Swap in tests; production
// wiring replaces it in library.go's init.
var execRunner executor = defaultExecutor{}

// runLangBlock executes the materialized source for a language block.
//
// Failure modes follow the project rule (extensions/README.md "Errors"):
//   - missing interpreter  -> preflight caught it; Resolve re-checks.
//   - subprocess failure   -> *ExecError with stderr
//   - decode failure       -> wrapped error with stdout excerpt
//   - internal wiring bug -> wrapped error
func runLangBlock(ctx context.Context, b *LangBlock, in any) (any, error) {
	lang, err := Lookup(b.ID, b.Version)
	if err != nil {
		return nil, fmt.Errorf("lang.%s: internal: registry lost language after parse: %w", b.ID, err)
	}

	path := b.SourceFile
	runBlock := b
	if path == "" {
		// Materialize inline source per invocation and clean it up even if the
		// interpreter fails or the context is canceled.
		dir, materializedPath, materializeErr := materialize(ctx, lang, b.Source)
		if materializeErr != nil {
			return nil, materializeErr
		}
		defer cleanupMaterialized(dir)
		path = materializedPath
	} else {
		// Re-read a file-backed script immediately before execution so its
		// dependency marker and the code handed to the interpreter agree.
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("lang.%s file %q: %w", lang.ID, path, readErr)
		}
		blockCopy := *b
		blockCopy.Source = string(source)
		runBlock = &blockCopy
	}

	cmd, args, err := resolveRunCmd(lang, runBlock, path)
	if err != nil {
		return nil, err
	}

	stdin, err := lang.Encode(in)
	if err != nil {
		return nil, fmt.Errorf("lang.%s encode: %w", lang.ID, err)
	}

	stdout, stderr, err := execRunner.Run(ctx, cmd, args, stdin)
	if err != nil {
		return nil, &ExecError{
			ID:     lang.ID,
			Cmd:    cmd,
			Cause:  err,
			Stderr: stderr,
		}
	}

	out, err := lang.Decode(stdout)
	if err != nil {
		return nil, fmt.Errorf("lang.%s decode: %w (stdout=%s)", lang.ID, err, truncate(stdout, 200))
	}
	return out, nil
}

// ExecError is the type returned when the interpreter subprocess fails.
// Stderr is a string (not []byte) — matches external.ExecResult.Stderr and avoids
// the []byte(string) copy.
type ExecError struct {
	ID     string
	Cmd    string
	Cause  error
	Stderr string
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("lang.%s: %s failed: %v\nstderr:\n%s",
		e.ID, e.Cmd, e.Cause, e.Stderr)
}

func (e *ExecError) Unwrap() error { return e.Cause }

// truncate returns at most n runes of s, with "..." if truncated.
// Used in error messages to avoid dumping megabytes of stdout.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
