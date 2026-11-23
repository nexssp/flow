package selfbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
)

// CPUProfileSummary identifies the saved profile and includes the readable
// output of `go tool pprof -top`. AnalysisError is non-fatal: the pprof file is
// still useful even when the Go tool or symbols are unavailable on this host.
type CPUProfileSummary struct {
	Path          string `json:"path"`
	TopFunctions  string `json:"top_functions,omitempty"`
	AnalysisError string `json:"analysis_error,omitempty"`
}

// CPUProfile controls one process-wide CPU profile. Go permits only one active
// CPU profile per process; call Stop before starting another profile.
type CPUProfile struct {
	ctx      context.Context
	file     *os.File
	path     string
	exe      string
	stopOnce sync.Once
	summary  CPUProfileSummary
	err      error
}

// StartCPUProfile starts recording CPU samples from the current process to
// path. The resulting file is a standard Go pprof profile.
func StartCPUProfile(path string) (*CPUProfile, error) {
	return startCPUProfile(context.Background(), path)
}

func startCPUProfile(ctx context.Context, path string) (*CPUProfile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("cpu profile path must not be empty")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve CPU profile path: %w", err)
	}
	file, err := os.OpenFile(absolutePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create CPU profile: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("locate current executable for CPU profile: %w", err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("start CPU profile: %w", err)
	}
	return &CPUProfile{ctx: ctx, file: file, path: absolutePath, exe: exe}, nil
}

// Stop flushes and closes the profile, then attempts to render its top
// functions. It is idempotent and safe to call from cleanup paths.
func (p *CPUProfile) Stop() (CPUProfileSummary, error) {
	if p == nil {
		return CPUProfileSummary{}, nil
	}
	p.stopOnce.Do(func() {
		pprof.StopCPUProfile()
		p.summary = CPUProfileSummary{Path: p.path}
		if err := p.file.Close(); err != nil {
			p.err = fmt.Errorf("close CPU profile: %w", err)
			return
		}
		p.renderTopFunctions()
	})
	return p.summary, p.err
}

func (p *CPUProfile) renderTopFunctions() {
	goTool, err := exec.LookPath("go")
	if err != nil {
		p.summary.AnalysisError = "go command not found; profile file was saved"
		return
	}
	command := exec.CommandContext(p.ctx, goTool, "tool", "pprof", "-top", "-nodecount=10", "-unit=ms", p.exe, p.path)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		p.summary.TopFunctions = strings.TrimSpace(output.String())
		p.summary.AnalysisError = fmt.Sprintf("go tool pprof: %v", err)
		return
	}
	p.summary.TopFunctions = strings.TrimSpace(output.String())
	if p.summary.TopFunctions == "" {
		p.summary.AnalysisError = "go tool pprof returned no summary"
	}
}
