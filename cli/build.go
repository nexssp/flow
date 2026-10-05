package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nexssp/flow/extensions/require"
)

// ── option types ──────────────────────────────────────────────────────

// buildOptions is the parsed command line for `nflow build`.
type buildOptions struct {
	output     string   // -o / --output
	debug      bool     // --debug: keep symbols and DWARF
	noTrimpath bool     // --no-trimpath: keep full source paths
	positional []string // the input .nflow path
}

// ── entry point ───────────────────────────────────────────────────────

// runBuild compiles a .nflow file into a standalone executable.
//
// Defaults favor distribution:
//
//   - symbols and DWARF stripped (-s -w) → 25–35% smaller binary
//   - trimpath on                       → no build-host paths in traces
//   - buildvcs off                      → no VCS metadata embedded
//   - version injected via -X           → `./myflow version` reports real values
//
// Flags are accepted in any position relative to the input path. The
// stdlib flag package stops at the first non-flag argument, which meant
// `nflow build file.nflow -o out` silently dropped -o; a manual parser
// closes that. Unknown flags fail loudly.
func runBuild(args []string) int {
	inv, err := newInvocation(nil, nil)
	if err != nil {
		return fatalf("flow host: %v", err)
	}
	return runInvocation(context.Background(), inv, func(ctx context.Context) int {
		return runBuildInInvocation(ctx, inv, args)
	})
}

func runBuildInInvocation(ctx context.Context, inv *invocation, args []string) int {
	started := time.Now()

	opts, rc := parseBuildArgs(args)
	if rc != 0 {
		return rc
	}
	if len(opts.positional) < 1 {
		return fatalf("usage: nflow build <file.nflow> [-o <output>] [--debug]")
	}

	nflowPath, err := resolveInputPath(opts.positional[0])
	if err != nil {
		return fatalf("%v", err)
	}

	fmt.Fprintf(os.Stderr, "📖 nflow: reading %s\n", filepath.Base(nflowPath))
	src, err := os.ReadFile(nflowPath)
	if err != nil {
		return buildFail("read", err)
	}

	fmt.Fprintf(os.Stderr, "🔎 nflow: resolving @require directives\n")
	reqs, err := inv.sourceRequiresFromFile(ctx, nflowPath)
	if err != nil {
		return buildFail("requires", err)
	}
	printRequires(reqs)

	binName, err := resolveBuildOutputPath(nflowPath, opts.output)
	if err != nil {
		return fatalf("%v", err)
	}
	fmt.Fprintf(os.Stderr, "📂 nflow: output %s\n", binName)

	fmt.Fprintf(os.Stderr, "🧱 nflow: staging build directory\n")
	buildDir, err := stageBuildDir(ctx, nflowPath, src, reqs)
	if err != nil {
		return buildFail("stage", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	tidyStart := time.Now()
	fmt.Fprintf(os.Stderr, "⚙️  nflow: resolving dependencies (go mod tidy -e)  — first run may download modules\n")
	if err = runGoContext(ctx, buildDir, "mod", "tidy", "-e"); err != nil {
		return buildFail("go mod tidy -e", err)
	}
	fmt.Fprintf(os.Stderr, "   ✓ dependencies resolved in %s\n",
		time.Since(tidyStart).Round(time.Millisecond))

	buildStart := time.Now()
	if err = linkBinary(ctx, buildDir, nflowPath, binName, opts); err != nil {
		return buildFail("build", err)
	}

	info, err := os.Stat(binName)
	if err != nil {
		return fatalf("built binary missing at %s: %v", binName, err)
	}

	fmt.Fprintf(os.Stderr,
		"✓ nflow: built %s (%s, linked in %s, total %s)\n",
		binName,
		humanBytes(info.Size()),
		time.Since(buildStart).Round(time.Millisecond),
		time.Since(started).Round(time.Millisecond),
	)
	return 0
}

// ── step helpers ──────────────────────────────────────────────────────

// resolveInputPath turns the positional argument into an absolute path
// and verifies the file exists. Both errors name the argument as the
// user typed it, not its absolute form, so the message reads the same
// as the input.
func resolveInputPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	return abs, nil
}

// printRequires writes one line per @require declaration. Centralized
// so the wording and the emoji prefix stay consistent with the other
// step messages.
func printRequires(reqs []require.Requirement) {
	if len(reqs) == 0 {
		fmt.Fprintf(os.Stderr, "   • no external modules\n")
		return
	}
	for i := range reqs {
		r := &reqs[i]
		switch {
		case r.IsLoose:
			fmt.Fprintf(os.Stderr, "   • loose package: %s\n", r.LocalPath)
		case r.IsLocal():
			fmt.Fprintf(os.Stderr, "   • local module: %s\n", r.Import)
		default:
			fmt.Fprintf(os.Stderr, "   • remote module: %s@%s\n", r.Import, r.Version)
		}
	}
}

// resolveOutputPath produces an absolute output path, applies the
// Windows .exe suffix, and creates the parent directory. The absolute
// path is load-bearing: `go build -o <relative>` resolves against the
// temp build directory, and the temp tree is deleted when the build
// returns.
func resolveBuildOutputPath(nflowPath, out string) (string, error) {
	binName := out
	if binName == "" {
		base := strings.TrimSuffix(filepath.Base(nflowPath), filepath.Ext(nflowPath))
		binName = base + exeSuffix()
	}

	abs, err := filepath.Abs(binName)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	abs = ensureExeSuffix(abs)

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create output directory %s: %w", filepath.Dir(abs), err)
	}
	return abs, nil
}

// stageBuildDir creates the temp directory and writes the three files
// the harness needs: the embedded source, the generated main.go, and
// the go.mod that links the driver and every @require.
//
// On any write failure the partially-staged directory is removed
// before the error is returned, so a caller that only cleans up on
// success does not leave a temp tree behind.
func stageBuildDir(ctx context.Context, nflowPath string, src []byte, reqs []require.Requirement) (string, error) {
	driverRoot, driverVersion := resolveDriverInfo()
	goworkPath := findGoWork(filepath.Dir(nflowPath))

	buildDir, err := os.MkdirTemp("", "nflow-build-*")
	if err != nil {
		return "", fmt.Errorf("temp: %w", err)
	}
	cleanupBuildDir := true
	defer func() {
		if cleanupBuildDir {
			_ = os.RemoveAll(buildDir)
		}
	}()

	flowDir := filepath.Dir(nflowPath)
	resolvedReqs, err := resolveRequirementPackagesWithContext(ctx, reqs, driverRoot, driverVersion, goworkPath, flowDir)
	if err != nil {
		return "", err
	}
	external := filterExternalRequires(resolvedReqs)
	imports, err := prepareBundleImports(buildDir, external)
	if err != nil {
		return "", fmt.Errorf("bundles: %w", err)
	}
	buildRoot, err := os.OpenRoot(buildDir)
	if err != nil {
		return "", fmt.Errorf("open build directory: %w", err)
	}
	defer func() { _ = buildRoot.Close() }()

	embeddedSource := rewriteEmbeddedRequires(string(src), filepath.Dir(nflowPath), resolvedReqs)
	if err := writeBuildFile(buildRoot, "workflow.nflow", []byte(embeddedSource)); err != nil {
		return "", fmt.Errorf("embed: %w", err)
	}

	if err := writeBuildFile(buildRoot, "main.go", []byte(buildMain(imports))); err != nil {
		return "", fmt.Errorf("main.go: %w", err)
	}

	mod := harnessGoMod(driverRoot, driverVersion, goworkPath, resolvedReqs, flowDir)
	if err := writeBuildFile(buildRoot, "go.mod", []byte(mod)); err != nil {
		return "", fmt.Errorf("go.mod: %w", err)
	}

	cleanupBuildDir = false
	return buildDir, nil
}

func writeBuildFile(root *os.Root, name string, contents []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(contents)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func rewriteEmbeddedRequires(source, flowDir string, requirements []require.Requirement) string {
	localImports := embeddedLocalImports(requirements)
	if len(localImports) == 0 {
		return source
	}

	lines := strings.SplitAfter(source, "\n")
	for i, line := range lines {
		if rewritten, ok := rewriteEmbeddedRequireLine(line, flowDir, localImports); ok {
			lines[i] = rewritten
		}
	}
	return strings.Join(lines, "")
}

func embeddedLocalImports(requirements []require.Requirement) map[string]string {
	imports := make(map[string]string)
	for i := range requirements {
		r := &requirements[i]
		if r.IsLocal() {
			imports[filepath.Clean(r.LocalPath)] = r.Import
		}
	}
	return imports
}

func rewriteEmbeddedRequireLine(line, flowDir string, localImports map[string]string) (string, bool) {
	start, end, ok := embeddedRequireTargetBounds(line)
	if !ok {
		return "", false
	}
	rawTarget := line[start:end]
	resolved, err := resolveEmbeddedRequireTarget(rawTarget, flowDir)
	if err != nil {
		return "", false
	}
	importPath, ok := localImports[resolved]
	if !ok {
		return "", false
	}
	if quote := embeddedRequireQuote(rawTarget); quote != 0 {
		importPath = string(quote) + importPath + string(quote)
	}
	return line[:start] + importPath + line[end:], true
}

func embeddedRequireTargetBounds(line string) (start, end int, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "@require") {
		return 0, 0, false
	}
	targetStart := len(line) - len(trimmed) + len("@require")
	for targetStart < len(line) && (line[targetStart] == ' ' || line[targetStart] == '\t') {
		targetStart++
	}
	targetEnd := targetStart
	for targetEnd < len(line) && !isEmbeddedRequireDelimiter(line[targetEnd]) {
		targetEnd++
	}
	return targetStart, targetEnd, targetStart != targetEnd
}

func isEmbeddedRequireDelimiter(char byte) bool {
	return char == ' ' || char == '\t' || char == '{' || char == '\n' || char == '\r'
}

func resolveEmbeddedRequireTarget(rawTarget, flowDir string) (string, error) {
	target := strings.Trim(rawTarget, `"'`)
	if !filepath.IsAbs(target) {
		target = filepath.Join(flowDir, target)
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func embeddedRequireQuote(rawTarget string) byte {
	if len(rawTarget) < 2 {
		return 0
	}
	quote := rawTarget[0]
	if (quote == '"' || quote == '\'') && rawTarget[len(rawTarget)-1] == quote {
		return quote
	}
	return 0
}

// linkBinary resolves the version metadata, builds the argv, announces
// the step, and runs `go build`.
func linkBinary(ctx context.Context, buildDir, nflowPath, binName string, opts buildOptions) error {
	version, commit, builtAt := resolveBuildVersion(ctx, filepath.Dir(nflowPath))
	ldflags := linkerFlags(opts, version, commit, builtAt)

	fmt.Fprintf(os.Stderr, "🔨 nflow: linking %s%s\n",
		filepath.Base(binName),
		buildModeDescription(opts),
	)
	return runGoContext(ctx, buildDir, buildArgs(binName, opts, ldflags)...)
}

// ── go build argv ─────────────────────────────────────────────────────

// buildArgs assembles the go build argv. The debug flag flips the two
// size-reduction defaults off; every other flag is fixed.
func buildArgs(binName string, opts buildOptions, ldflags string) []string {
	args := []string{"build"}

	if !opts.noTrimpath {
		args = append(args, "-trimpath")
	}
	if !opts.debug {
		args = append(args,
			"-buildvcs=false",
			"-ldflags="+ldflags,
		)
	}

	args = append(args, "-o", binName, ".")
	return args
}

// linkerFlags returns the -ldflags value. version/commit/builtAt are
// passed in rather than resolved here so the caller controls the
// git-fallback policy.
func linkerFlags(opts buildOptions, version, commit, builtAt string) string {
	flags := []string{
		"-X github.com/nexssp/flow/cli.Version=" + version,
		"-X github.com/nexssp/flow/cli.Commit=" + commit,
		"-X github.com/nexssp/flow/cli.BuiltAt=" + builtAt,
	}
	if !opts.debug {
		// -s drops the symbol table; -w drops DWARF. Together they
		// remove the debugging metadata that accounts for the majority
		// of a Go binary's size beyond its code.
		flags = append([]string{"-s", "-w"}, flags...)
	}
	return strings.Join(flags, " ")
}

func buildModeDescription(opts buildOptions) string {
	switch {
	case opts.debug:
		return "  (debug — symbols kept)"
	case opts.noTrimpath:
		return "  (release, full paths)"
	default:
		return "  (release, symbols stripped)"
	}
}

// ── version resolution ────────────────────────────────────────────────

// resolveBuildVersion returns version metadata for the packaged binary.
//
// Priority:
//
//  1. The running CLI's linker-injected values (nflow self up, or a
//     release binary built with -ldflags="-X ...").
//  2. The running CLI's embedded VCS info (debug.ReadBuildInfo).
//  3. Git in the flow's own directory.
//
// The third fallback is what makes `nflow build` invoked via `go run`
// produce a binary that reports a real commit hash instead of "dev
// unknown". The running CLI has nothing useful to say about itself in
// that case; the flow's repository does.
//
// Each git call runs under its own short timeout. A git invocation that
// hangs — a credential prompt, a lock file, a corrupt object store —
// must not block the build. On any failure the field is left as-is and
// the build proceeds.
func resolveBuildVersion(ctx context.Context, flowDir string) (version, commit, builtAt string) {
	version, commit, builtAt = resolvedBuildInfo()

	if flowDir == "" {
		return version, commit, builtAt
	}

	if version == "" || version == "dev" || version == "unknown" {
		gitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if v := gitDescribe(gitCtx, flowDir); v != "" && v != "dev" {
			version = v
		}
		cancel()
	}

	if commit == "" || commit == unknownValue {
		gitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if c := gitCommit(gitCtx, flowDir); c != "" && c != unknownValue {
			commit = c
		}
		cancel()
	}

	if builtAt == "" || builtAt == unknownValue {
		builtAt = time.Now().UTC().Format(time.RFC3339)
	}

	return version, commit, builtAt
}

// ── output path helpers ───────────────────────────────────────────────

// ensureExeSuffix appends ".exe" on Windows when the path does not
// already end in that extension, case-insensitively. Non-Windows
// platforms are unaffected.
func ensureExeSuffix(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return path
	}
	return path + ".exe"
}

// humanBytes renders a byte count in binary units.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for nn := n / unit; nn >= unit; nn /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ── argument parsing ──────────────────────────────────────────────────

// parseBuildArgs reads the build command's flags in any position.
// Returns (options, 0) on success, (_, rc) on error or --help, where
// rc is the process exit code to propagate.
func parseBuildArgs(args []string) (opts buildOptions, rc int) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--output":
			if i+1 >= len(args) {
				return opts, fatalf("build: flag %s requires a value", a)
			}
			i++
			opts.output = args[i]
		case strings.HasPrefix(a, "-o="):
			opts.output = strings.TrimPrefix(a, "-o=")
		case strings.HasPrefix(a, "--output="):
			opts.output = strings.TrimPrefix(a, "--output=")

		case a == "--debug" || a == "-d":
			opts.debug = true
		case a == "--no-trimpath":
			opts.noTrimpath = true

		case a == flagHelp || a == flagHelpShort:
			printBuildHelp()
			return opts, 0

		case strings.HasPrefix(a, "-"):
			return opts, fatalf("build: unknown flag %s", a)

		default:
			opts.positional = append(opts.positional, a)
		}
	}
	return opts, 0
}

func printBuildHelp() {
	fmt.Fprint(os.Stderr, `Usage: nflow build <file.nflow> [-o <output>] [--debug]

Compile a .nflow file into a standalone executable. The binary embeds
the source and links every @require module. It runs the flow when
launched and accepts the same JSON payload and CLI flags as 'nflow run'.

Flags:
  -o, --output <path>   Output binary path. Defaults to the source
                        filename; on Windows, ".exe" is appended
                        automatically if missing.

  --debug, -d           Keep symbols and DWARF debug info. Larger
                        binary (typically +30-40%), real stack traces.

  --no-trimpath         Keep full source paths in the binary. Off by
                        default; useful when the binary and its source
                        tree live on the same machine.

Defaults favor distribution: symbols stripped (-s -w), trimpath on,
buildvcs off, and version metadata injected from the running CLI.

Examples:
  nflow build flow.nflow
  nflow build flow.nflow -o ./bin/my-flow
  nflow build -o ./bin/my-flow flow.nflow
  nflow build flow.nflow -o ./bin/my-flow --debug

Cross-compilation: this command targets the host platform. For a
different target, invoke 'go build' on the harness the CLI stages.
`)
}

// ── error rendering ───────────────────────────────────────────────────

// buildFail renders a step failure with the step label and the error.
func buildFail(step string, err error) int {
	fmt.Fprintf(os.Stderr, "✗ nflow: %s: %v\n", step, err)
	return 1
}

// ── generated main.go ─────────────────────────────────────────────────

// buildMain is the source of the generated main.go that lives in the
// temp build directory. It embeds the .nflow file and hands control to
// cli.RunEmbeddedWithBundles, which compiles and runs it with the statically
// linked @require bundles.
func buildMain(imports []bundleImport) string {
	var mainBuilder strings.Builder
	mainBuilder.WriteString(`package main

import (
	"context"
	_ "embed"
	"os"

	"github.com/nexssp/flow/cli"
	"github.com/nexssp/flow/core"
`)
	writeBundleImports(&mainBuilder, imports)
	mainBuilder.WriteString(`)

//go:embed workflow.nflow
var embedded string

func main() {
	`)
	writeBundleTargets(&mainBuilder, imports)
	mainBuilder.WriteString(`	os.Exit(cli.RunEmbeddedWithBundleFactoriesForRequirements(context.Background(), embedded, os.Args[1:], targets, func(adopt func(core.Bundle) error) error {
`)
	writeBundleConstruction(&mainBuilder, imports)
	mainBuilder.WriteString(`		return nil
    }))
}
`)
	return mainBuilder.String()
}
