package selfbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCPUProfileWritesPprofAndTopFunctions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.cpu.pprof")
	profiler, err := StartCPUProfile(path)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	var work uint64
	for time.Now().Before(deadline) {
		for range 20_000 {
			work = work*1664525 + 1013904223
		}
	}
	runtime.KeepAlive(work)

	summary, err := profiler.Stop()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("CPU profile file is empty")
	}
	if summary.Path != path {
		t.Fatalf("summary path = %q, want %q", summary.Path, path)
	}
	if summary.TopFunctions == "" {
		t.Fatalf("top-functions summary missing: %s", summary.AnalysisError)
	}
	if !strings.Contains(summary.TopFunctions, "Total samples =") || !strings.Contains(summary.TopFunctions, "flat  flat%") {
		t.Fatalf("unexpected pprof summary: %s", summary.TopFunctions)
	}
	second, err := profiler.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if second != summary {
		t.Fatalf("second Stop() changed summary: %+v != %+v", second, summary)
	}
}

func TestStartCPUProfileRejectsEmptyPath(t *testing.T) {
	if _, err := StartCPUProfile(" "); err == nil {
		t.Fatal("StartCPUProfile accepted an empty path")
	}
}
