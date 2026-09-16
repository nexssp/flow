package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/expr-lang/expr"
	"github.com/joho/godotenv"
	"github.com/nexssp/flow"
	flowcontracts "github.com/nexssp/flow/contracts"
	flowrunner "github.com/nexssp/flow/runner"
	"github.com/nexssp/flow/runner/bootstrap/console"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type ExecutionAuditRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	Action     string    `json:"action"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	Payload    any       `json:"payload"`
	Result     any       `json:"result,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	Assertions []string  `json:"assertions,omitempty"`
}

type App struct {
	Name    string
	Version string
	Port    string
	WorkDir string
	Actions []action.AnyAction

	libraries []action.Library

	loaders []loader
}

func New(name, version string) *App {
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	workDir := os.Getenv("WORKSPACE_DIR")
	if workDir == "" {
		workDir = ".nexss_workspace"
	}

	// workDir is derived from WORKSPACE_DIR, a trusted local env var of
	// this process; it is not request input. Gosec G703 cannot distinguish
	// the two trust categories.
	//nolint:gosec // G703: trusted local env, not user input
	_ = os.MkdirAll(filepath.Join(workDir, "runs"), 0o755)

	return &App{
		Name:    name,
		Version: version,
		Port:    port,
		WorkDir: workDir,
	}
}

func (a *App) Mount(actions ...action.AnyAction) *App {
	a.Actions = append(a.Actions, actions...)

	return a
}

func (a *App) WithLoader(l Loader) *App {
	if l != nil {
		a.loaders = append(a.loaders, loader(l))
	}

	return a
}

// Run is a thin wrapper. It exists so that the deferred signal-context
// cleanup inside run() always executes on the normal return path.
// gocritic exitAfterDefer: os.Exit must not be called from a scope that
// holds a deferred stop().
func (a *App) Run() {
	os.Exit(a.run())
}

// run owns the signal context and returns an exit code. It never calls
// os.Exit itself, so every defer runs when it returns.
func (a *App) run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(a.libraries) > 0 {
		reg, err := action.NewRegistry(a.libraries...)
		if err != nil {
			slog.Error("bootstrap: library registry build failed", "err", err)

			return 1
		}

		a.Actions = append(a.Actions, reg.Actions()...)
	}

	asm := newAssembly()

	asm.Actions = append(asm.Actions, a.Actions...)

	for _, load := range a.loaders {
		if err := load(asm); err != nil {
			slog.Error("assembly failed", "err", err)

			return 1
		}
	}

	a.Actions = asm.finalize()

	var (
		assertions []string
		cleanArgs  []string
	)

	for _, arg := range os.Args {
		switch {
		case strings.HasPrefix(arg, "--assert="):
			assertions = append(assertions, strings.TrimPrefix(arg, "--assert="))
		case strings.HasPrefix(arg, "--testkit="):
			assertions = append(assertions, strings.TrimPrefix(arg, "--testkit="))
		default:
			cleanArgs = append(cleanArgs, arg)
		}
	}

	if len(cleanArgs) >= 2 && cleanArgs[1] == "run" {
		return a.runCLI(ctx, cleanArgs, assertions)
	}

	if len(cleanArgs) >= 3 && cleanArgs[1] == "flow" {
		return a.runFlow(ctx, cleanArgs)
	}

	server := thttp.New(":" + a.Port)
	server.Mount(a.Actions)
	slog.Info(fmt.Sprintf("🌟 %s v%s :: Online at http://localhost:%s",
		strings.ToUpper(a.Name), a.Version, a.Port), "actions", len(a.Actions))

	go func() {
		if _, err := server.Do(ctx, nil); err != nil && err != http.ErrServerClosed {
			slog.Error("server runtime error", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info(fmt.Sprintf("Shutting down %s gracefully...", a.Name))
	time.Sleep(100 * time.Millisecond)

	return 0
}

func (a *App) runCLI(ctx context.Context, args, assertions []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "❌ Usage: run <action.name> [json_payload]")

		return 1
	}

	targetName := args[2]

	rawJSON := "{}"
	if len(args) >= 4 {
		rawJSON = args[3]
	}

	fmt.Printf("\n🚀 %s HEADLESS CLI\n🎯 Action: %s\n", strings.ToUpper(a.Name), targetName)
	fmt.Println(strings.Repeat("─", 80))

	var targetAct action.AnyAction

	for _, act := range a.Actions {
		if act.Describe().Name == targetName {
			targetAct = act

			break
		}
	}

	if targetAct == nil {
		fmt.Fprintf(os.Stderr, "❌ Action %q not found in registry\n", targetName)
		a.dumpError(targetName, "action not found in registry", rawJSON)

		return 1
	}

	var payload any
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Invalid JSON payload: %v\n", err)
		a.dumpError(targetName, fmt.Sprintf("invalid JSON payload: %v", err), rawJSON)

		return 1
	}

	cliReg, regErr := action.NewRegistry(action.Of(a.Actions...))
	if regErr != nil {
		fmt.Fprintf(os.Stderr, "❌ registry build failed: %v\n", regErr)

		return 1
	}

	ctx = flowcontracts.WithRegistry(ctx, cliReg)
	ctx = flowcontracts.WithCompiler(ctx, flow.NewCompiler(cliReg))

	start := time.Now()
	res, err := action.InvokeAny(ctx, targetAct, payload)
	duration := time.Since(start).Milliseconds()

	fmt.Println(strings.Repeat("─", 80))

	audit := ExecutionAuditRecord{
		Timestamp:  time.Now().UTC(),
		Action:     targetName,
		Success:    err == nil,
		Payload:    payload,
		Result:     res,
		DurationMs: duration,
		Assertions: assertions,
	}

	if err != nil {
		audit.Error = err.Error()
		a.saveRunArtifact(audit)
		a.dumpError(targetName, err.Error(), res)
		fmt.Printf("❌ EXECUTION FAILED: %v\n", err)

		return 1
	}

	a.saveRunArtifact(audit)

	outJSON, _ := json.MarshalIndent(res, "", "  ")

	fmt.Println("✅ EXECUTION SUCCESS. Output:")
	fmt.Println(string(outJSON))

	return runCliAssertions(res, assertions)
}

func (a *App) runFlow(ctx context.Context, args []string) int {
	flowPath := args[2]

	runnerArgs := []string{}
	payload := map[string]any{}

	for _, arg := range args[3:] {
		if strings.HasPrefix(arg, "{") {
			if err := json.Unmarshal([]byte(arg), &payload); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Invalid JSON payload: %v\n", err)

				return 1
			}

			continue
		}

		runnerArgs = append(runnerArgs, arg)
	}

	reg, err := action.NewRegistry(action.Of(a.Actions...))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ registry build failed: %v\n", err)

		return 1
	}

	return flowrunner.Default{}.RunWithRegistry(ctx, flowrunner.Request{
		Path:    flowPath,
		Payload: payload,
		Args:    runnerArgs,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}, reg, nil)
}

func (a *App) saveRunArtifact(record ExecutionAuditRecord) {
	filename := fmt.Sprintf("run_%s_%d.json",
		time.Now().Format("20060102_150405"),
		time.Now().UnixNano()%100000)
	dest := filepath.Join(a.WorkDir, "runs", filename)

	if data, err := json.MarshalIndent(record, "", "  "); err == nil {
		// 0600: audit records carry payload, result, and assertions, which
		// may include user prompts and model responses.
		_ = os.WriteFile(dest, data, 0o600)
		slog.Info("artifact persisted", "file", dest)
	}
}

func (a *App) dumpError(target, errStr string, contextData any) {
	logPath := filepath.Join(a.WorkDir, "error.log")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	dataJSON, _ := json.Marshal(contextData)

	entry := fmt.Sprintf("[%s] ❌ CRASH DUMP | Target: %s | Error: %s | Context: %s\n",
		ts, target, errStr, string(dataJSON))
	_, _ = f.WriteString(entry)

	fmt.Printf("📑 Emergency trace appended to: %s\n", logPath)
}

func runCliAssertions(result any, assertions []string) int {
	if len(assertions) == 0 {
		return 0
	}

	fmt.Printf("\n🧪 RUNNING TESTKIT ASSERTIONS (%d)...\n", len(assertions))

	b, _ := json.Marshal(result)

	var env map[string]any

	_ = json.Unmarshal(b, &env)

	failed := false

	for _, exprStr := range assertions {
		program, err := expr.Compile(exprStr, expr.Env(env))
		if err != nil {
			fmt.Printf("  ❌ ERROR: Invalid syntax -> %s\n     (%v)\n", exprStr, err)

			failed = true

			continue
		}

		out, err := expr.Run(program, env)
		if err != nil {
			fmt.Printf("  ❌ ERROR: Runtime error -> %s\n     (%v)\n", exprStr, err)

			failed = true

			continue
		}

		if passed, ok := out.(bool); ok && passed {
			fmt.Printf("  ✅ PASS: %s\n", exprStr)
		} else {
			fmt.Printf("  ❌ FAIL: %s (Evaluated to: %v)\n", exprStr, out)

			failed = true
		}
	}

	fmt.Println(strings.Repeat("─", 80))

	if failed {
		fmt.Println("🛑 TESTKIT FAILED.")

		return 1
	}

	fmt.Println("🎉 ALL TESTKIT ASSERTIONS PASSED.")

	return 0
}

func (a *App) WithConsole(opts ...ConsoleOption) *App {
	cfg := console.Config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	a.loaders = append(a.loaders, func(asm *Assembly) error {
		reg := console.RegistryFunc(func() []action.AnyAction { return asm.Actions })
		asm.Actions = append(asm.Actions, console.Actions(reg, cfg)...)

		return nil
	})

	return a
}

type ConsoleOption func(*console.Config)

func WithConsoleTitle(title string) ConsoleOption {
	return func(c *console.Config) { c.Title = title }
}

func WithConsoleBasePath(path string) ConsoleOption {
	return func(c *console.Config) { c.BasePath = path }
}

func WithConsoleExecute() ConsoleOption {
	return func(c *console.Config) { c.AllowExecute = true }
}

func (a *App) WithLibrary(lib action.Library) *App {
	a.libraries = append(a.libraries, lib)

	return a
}

func (a *App) WithLibraries(libs ...action.Library) *App {
	a.libraries = append(a.libraries, libs...)

	return a
}
