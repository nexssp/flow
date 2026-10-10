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

type buildOptions struct {
	output     string
	debug      bool
	noTrimpath bool
	allBundles bool
	positional []string
}

type nativeBundleInfo struct {
	id      string
	pkg     string
	matcher func(src string) bool
}

var allNativeBundles = []nativeBundleInfo{
	{
		id:      "syntax",
		pkg:     "github.com/nexssp/flow/extensions/syntax",
		matcher: func(string) bool { return true },
	},
	{
		id:      "runtime",
		pkg:     "github.com/nexssp/flow/extensions/runtime",
		matcher: func(string) bool { return true },
	},
	{
		id:      "description",
		pkg:     "github.com/nexssp/flow/extensions/description",
		matcher: func(string) bool { return true },
	},
	{
		id:      "require",
		pkg:     "github.com/nexssp/flow/extensions/require",
		matcher: func(string) bool { return true },
	},
	{
		id:      "pipeline",
		pkg:     "github.com/nexssp/flow/extensions/pipeline",
		matcher: func(s string) bool { return strings.Contains(s, "@pipeline") || strings.Contains(s, "pipeline.") },
	},
	{
		id:      "modifiers_core",
		pkg:     "github.com/nexssp/flow/extensions/modifiers_core",
		matcher: func(string) bool { return true },
	},
	{
		id:      "modifiers_meta",
		pkg:     "github.com/nexssp/flow/extensions/modifiers_meta",
		matcher: func(string) bool { return true },
	},
	{
		id:  "modifiers_auth",
		pkg: "github.com/nexssp/flow/extensions/modifiers_auth",
		matcher: func(s string) bool {
			return strings.Contains(s, ":auth") || strings.Contains(s, ":role") ||
				strings.Contains(s, ":perm") || strings.Contains(s, ":feature")
		},
	},
	{
		id:      "match",
		pkg:     "github.com/nexssp/flow/extensions/match",
		matcher: func(s string) bool { return strings.Contains(s, "match") },
	},
	{
		id:  "assert",
		pkg: "github.com/nexssp/flow/extensions/assert",
		matcher: func(s string) bool {
			return strings.Contains(s, "@assert") || strings.Contains(s, "assert(") || strings.Contains(s, "assert ")
		},
	},
	{
		id:      "loop",
		pkg:     "github.com/nexssp/flow/extensions/loop",
		matcher: func(s string) bool { return strings.Contains(s, "loop(") || strings.Contains(s, "loop ") },
	},
	{
		id:      "projection",
		pkg:     "github.com/nexssp/flow/extensions/projection",
		matcher: func(s string) bool { return strings.Contains(s, "{") },
	},
	{
		id:      "config",
		pkg:     "github.com/nexssp/flow/extensions/config",
		matcher: func(s string) bool { return strings.Contains(s, "@config") },
	},
	{
		id:      "constants",
		pkg:     "github.com/nexssp/flow/extensions/constants",
		matcher: func(s string) bool { return strings.Contains(s, "@const") || strings.Contains(s, "${") },
	},
	{
		id:      "flow_version",
		pkg:     "github.com/nexssp/flow/extensions/flow_version",
		matcher: func(s string) bool { return strings.Contains(s, "@flow_version") },
	},
	{
		id:  "schema",
		pkg: "github.com/nexssp/flow/extensions/schema",
		matcher: func(s string) bool {
			return strings.Contains(s, "@schema") || strings.Contains(s, ":schema") || strings.Contains(s, "schema.")
		},
	},
	{
		id:  "scope",
		pkg: "github.com/nexssp/flow/extensions/scope",
		matcher: func(s string) bool {
			return strings.Contains(s, "@scope") || strings.Contains(s, "@profile") || strings.Contains(s, ":profile")
		},
	},
	{
		id:      "pool",
		pkg:     "github.com/nexssp/flow/extensions/pool",
		matcher: func(s string) bool { return strings.Contains(s, "@pool") || strings.Contains(s, "pool.") },
	},
	{
		id:      "macros",
		pkg:     "github.com/nexssp/flow/extensions/macros",
		matcher: func(s string) bool { return strings.Contains(s, "@macro") },
	},
	{
		id:      "include",
		pkg:     "github.com/nexssp/flow/extensions/include",
		matcher: func(s string) bool { return strings.Contains(s, "@include") },
	},
	{
		id:      "on",
		pkg:     "github.com/nexssp/flow/extensions/on",
		matcher: func(s string) bool { return strings.Contains(s, "@on") },
	},
	{
		id:      "on_error",
		pkg:     "github.com/nexssp/flow/extensions/on_error",
		matcher: func(s string) bool { return strings.Contains(s, "@on_error") || strings.Contains(s, "on_error") },
	},
	{
		id:      "retry",
		pkg:     "github.com/nexssp/flow/extensions/retry",
		matcher: func(s string) bool { return strings.Contains(s, ":retry") },
	},
	{
		id:      "hook",
		pkg:     "github.com/nexssp/flow/extensions/hook",
		matcher: func(s string) bool { return strings.Contains(s, "@hook") || strings.Contains(s, "hook.") },
	},
	{
		id:  "fs",
		pkg: "github.com/nexssp/flow/extensions/fs",
		matcher: func(s string) bool {
			return strings.Contains(s, "fs.") || strings.Contains(s, "out.file") || strings.Contains(s, "out.stdout")
		},
	},
	{
		id:      "io",
		pkg:     "github.com/nexssp/flow/extensions/io",
		matcher: func(s string) bool { return strings.Contains(s, "io.") },
	},
	{
		id:  "external",
		pkg: "github.com/nexssp/flow/extensions/external",
		matcher: func(s string) bool {
			return strings.Contains(s, "external.") || strings.Contains(s, "http.request")
		},
	},
	{
		id:      "nodes_log",
		pkg:     "github.com/nexssp/flow/extensions/nodes_log",
		matcher: func(s string) bool { return strings.Contains(s, "log.") },
	},
	{
		id:      "nodes_bench",
		pkg:     "github.com/nexssp/flow/extensions/nodes_bench",
		matcher: func(s string) bool { return strings.Contains(s, "bench.") },
	},
	{
		id:      "nodes_distribute",
		pkg:     "github.com/nexssp/flow/extensions/nodes_distribute",
		matcher: func(s string) bool { return strings.Contains(s, "distribute.") },
	},
	{
		id:      "nodes_dispatch",
		pkg:     "github.com/nexssp/flow/extensions/nodes_dispatch",
		matcher: func(s string) bool { return strings.Contains(s, "dispatch.") },
	},
	{
		id:      "nodes_supervisor",
		pkg:     "github.com/nexssp/flow/extensions/nodes_supervisor",
		matcher: func(s string) bool { return strings.Contains(s, "supervisor.") },
	},
	{
		id:      "realtime",
		pkg:     "github.com/nexssp/flow/extensions/realtime",
		matcher: func(s string) bool { return strings.Contains(s, "realtime.") },
	},
	{
		id:      "stream_ops",
		pkg:     "github.com/nexssp/flow/extensions/stream_ops",
		matcher: func(s string) bool { return strings.Contains(s, "stream.") },
	},
	{
		id:      "render",
		pkg:     "github.com/nexssp/flow/extensions/render",
		matcher: func(s string) bool { return strings.Contains(s, "render.") },
	},
	{
		id:      "selftestkit",
		pkg:     "github.com/nexssp/flow/extensions/selftestkit",
		matcher: func(s string) bool { return strings.Contains(s, "cov.") || strings.Contains(s, "selftestkit") },
	},
}

func detectUsedNativeBundles(src string, forceAll bool) []nativeBundleInfo {
	if forceAll {
		return append([]nativeBundleInfo(nil), allNativeBundles...)
	}

	used := make([]nativeBundleInfo, 0, len(allNativeBundles))
	for _, b := range allNativeBundles {
		if b.matcher(src) {
			used = append(used, b)
		}
	}
	return used
}

func runBuild(args []string) int {
	inv, err := newNativeInvocation(nil, nil)
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
	buildDir, err := stageBuildDir(ctx, nflowPath, src, reqs, opts)
	if err != nil {
		return buildFail("stage", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	tidyStart := time.Now()
	fmt.Fprintf(os.Stderr, "⚙️  nflow: resolving dependencies (go mod tidy)  — first run may download modules\n")
	if err = runGoContext(ctx, buildDir, "mod", "tidy"); err != nil {
		return buildFail("go mod tidy", err)
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

func stageBuildDir(
	ctx context.Context,
	nflowPath string,
	src []byte,
	reqs []require.Requirement,
	opts buildOptions,
) (string, error) {
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

	usedNative := detectUsedNativeBundles(embeddedSource, opts.allBundles)

	if err := writeBuildFile(buildRoot, "main.go", []byte(buildMain(usedNative, imports))); err != nil {
		return "", fmt.Errorf("main.go: %w", err)
	}

	mod := harnessGoMod(driverRoot, driverVersion, goworkPath, external, flowDir)
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

func linkBinary(ctx context.Context, buildDir, nflowPath, binName string, opts buildOptions) error {
	version, commit, builtAt := resolveBuildVersion(ctx, filepath.Dir(nflowPath))
	ldflags := linkerFlags(opts, version, commit, builtAt)

	fmt.Fprintf(os.Stderr, "🔨 nflow: linking %s%s\n",
		filepath.Base(binName),
		buildModeDescription(opts),
	)
	return runGoContext(ctx, buildDir, buildArgs(binName, opts, ldflags)...)
}

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

func linkerFlags(opts buildOptions, version, commit, builtAt string) string {
	flags := []string{
		"-X main.Version=" + version,
		"-X main.Commit=" + commit,
		"-X main.BuiltAt=" + builtAt,
	}
	if !opts.debug {
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
		return "  (release, tree-shaken, symbols stripped)"
	}
}

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

func ensureExeSuffix(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return path
	}
	return path + ".exe"
}

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
		case a == "--all-bundles":
			opts.allBundles = true

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
	fmt.Fprint(os.Stderr, `Usage: nflow build <file.nflow> [-o <output>] [--debug] [--all-bundles]

Compile a .nflow file into a standalone, tree-shaken executable. The binary
embeds the workflow and links only the required native and @require modules.
It executes with zero compiler/CLI runtime baggage (~2MB binary size).

Flags:
  -o, --output <path>   Output binary path. Defaults to the source
                        filename; on Windows, ".exe" is appended
                        automatically if missing.

  --debug, -d           Keep symbols and DWARF debug info. Larger
                        binary (typically +30-40%), real stack traces.

  --no-trimpath         Keep full source paths in the binary. Off by
                        default; useful when the binary and its source
                        tree live on the same machine.

  --all-bundles         Link all native extensions instead of tree-shaking
                        (increases binary size by ~18MB).

Defaults favor lean distribution: dead-code elimination enabled, symbols
stripped (-s -w), trimpath on, and buildvcs off.
`)
}

func buildFail(step string, err error) int {
	fmt.Fprintf(os.Stderr, "✗ nflow: %s: %v\n", step, err)
	return 1
}

func buildMain(usedBundles []nativeBundleInfo, imports []bundleImport) string {
	var b strings.Builder
	b.WriteString(`package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/flow/runner"
`)

	for i, bundle := range usedBundles {
		fmt.Fprintf(&b, "\tnative%d %q\n", i, bundle.pkg)
	}
	writeBundleImports(&b, imports)

	b.WriteString(`)

//go:embed workflow.nflow
var embedded string

var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version":
			fmt.Printf("flow binary (version: %s, commit: %s, built at: %s)\n", Version, Commit, BuiltAt)
			os.Exit(0)
		case "help", "--help", "-h":
			fmt.Println("Nexss Flow standalone executable.")
			fmt.Println("Usage:\n  <binary> [json_payload] [flags]")
			os.Exit(0)
		}
	}

	payload := map[string]any{}
	var flags []string
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "{") {
			_ = json.Unmarshal([]byte(arg), &payload)
		} else if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		}
	}

	bundles := []core.Bundle{
`)
	for i := range usedBundles {
		fmt.Fprintf(&b, "\t\tnative%d.Bundle(nil),\n", i)
	}
	b.WriteString(`	}

	host := runner.NewHost()
	defer func() { _ = host.Shutdown(context.Background()) }()
	if err := host.OwnAll(bundles); err != nil {
		fmt.Fprintf(os.Stderr, "host error: %v\n", err)
		os.Exit(2)
	}
`)

	if len(imports) > 0 {
		b.WriteString(`
	adopt := func(bundle core.Bundle) error {
		if err := host.Own(bundle); err != nil {
			return err
		}
		bundles = append(bundles, bundle)
		return nil
	}

	constructExternal := func() error {
`)
		writeBundleConstruction(&b, imports)
		b.WriteString(`		return nil
	}
	if err := constructExternal(); err != nil {
		fmt.Fprintf(os.Stderr, "bundle error: %v\n", err)
		os.Exit(2)
	}
`)
	}

	b.WriteString(`
	cfg, err := runner.BuildConfig(bundles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	opts := runner.Opts{
		Verbosity: countVerbosity(flags),
	}
	os.Exit(runner.RunSource(context.Background(), cfg, embedded, "<embedded>", payload, opts))
}

func countVerbosity(flags []string) int {
	n := 0
	for _, f := range flags {
		if f == "--verbose" {
			n++
			continue
		}
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") {
			vCount := strings.Count(f, "v")
			if vCount > 0 && vCount == len(f)-1 {
				n += vCount
			}
		}
	}
	return n
}
`)
	return b.String()
}
