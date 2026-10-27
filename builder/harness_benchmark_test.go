package builder

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nexssp/flow/extensions/require"
)

// BenchmarkHarnessBuildModes compares the thin and CLI harnesses for the same
// small @require ./helpers flow. Each build gets a fresh runner cache and a
// separate empty Go build cache, so the measured modes do not warm each other.
// Use -benchtime=1x to run one cold build per mode.
func BenchmarkHarnessBuildModes(b *testing.B) {
	flowPath, reqs := benchmarkHarnessFixture(b)
	b.Setenv("NFLOW_HARNESS_TIMINGS", "1")

	for _, mode := range []harnessMode{harnessModeThin, harnessModeCLI} {
		b.Run(string(mode)+"/fresh_runner_and_go_cache", func(b *testing.B) {
			cacheRoot := b.TempDir()
			b.Setenv(harnessCacheEnv, cacheRoot)
			var binaryBytes int64

			for range b.N {
				b.StopTimer()
				if err := os.RemoveAll(cacheRoot); err != nil {
					b.Fatal(err)
				}
				goCache := b.TempDir()
				b.Setenv("GOCACHE", goCache)
				b.StartTimer()

				bin, err := ensureHarnessContext(context.Background(), mode, reqs, flowPath)

				b.StopTimer()
				_ = os.RemoveAll(goCache)
				if err != nil {
					b.Fatal(err)
				}
				info, err := os.Stat(bin)
				if err != nil {
					b.Fatal(err)
				}
				binaryBytes = info.Size()
				b.StartTimer()
			}
			b.StopTimer()
			b.ReportMetric(float64(binaryBytes), "binary_bytes")
		})
	}
}

// BenchmarkHarnessWarmRuns measures launching already-built cached harness
// binaries. It is separate from the cold build benchmark so -benchtime=50x can
// collect stable warm-run latency without rebuilding the harness 50 times.
func BenchmarkHarnessWarmRuns(b *testing.B) {
	flowPath, reqs := benchmarkHarnessFixture(b)

	for _, mode := range []harnessMode{harnessModeThin, harnessModeCLI} {
		b.Run(string(mode)+"/cached_binary", func(b *testing.B) {
			b.Setenv(harnessCacheEnv, b.TempDir())
			bin, err := ensureHarnessContext(context.Background(), mode, reqs, flowPath)
			if err != nil {
				b.Fatal(err)
			}
			info, err := os.Stat(bin)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(info.Size()), "binary_bytes")

			b.ResetTimer()
			for range b.N {
				runBenchmarkHarness(b, bin, flowPath)
			}
		})
	}
}

func benchmarkHarnessFixture(b *testing.B) (string, []require.Requirement) {
	b.Helper()
	workDir := b.TempDir()
	helperDir := filepath.Join(workDir, "helpers")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		b.Fatal(err)
	}
	helperSource := `package helpers

import (
	"context"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

const ID = "helpers"

func init() { compiler.Register(ID, Bundle) }

func Bundle(_ map[string]string) compiler.Bundle {
	return compiler.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name: ID,
			Actions: []action.AnyAction{
				action.New("helpers.echo", func(context.Context, any) (string, error) {
					return "benchmark", nil
				}).Build(),
			},
		}},
	}
}
`
	if err := os.WriteFile(filepath.Join(helperDir, "library.go"), []byte(helperSource), 0o600); err != nil {
		b.Fatal(err)
	}
	flowPath := filepath.Join(workDir, "bench.nflow")
	flow := `@require ./helpers
@assert: result == "benchmark"
{} -> helpers.echo
`
	if err := os.WriteFile(flowPath, []byte(flow), 0o600); err != nil {
		b.Fatal(err)
	}
	reqs, err := SourceRequires(context.Background(), flowPath)
	if err != nil {
		b.Fatal(err)
	}
	return flowPath, reqs
}

func runBenchmarkHarness(b *testing.B, bin, flowPath string) {
	b.Helper()
	cmd := exec.CommandContext(context.Background(), bin, "run", flowPath, "--json")
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		b.Fatal(err)
	}
	defer stdin.Close()
	cmd.Stdin = stdin
	cmd.Env = append(os.Environ(), harnessEnv+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		b.Fatalf("run cached %s harness: %v\nstdout: %s\nstderr: %s", filepath.Base(bin), err, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("benchmark")) {
		b.Fatalf("cached harness output missing benchmark result: %s", stdout.String())
	}
}
