package selftest

import (
	"fmt"
	"runtime"
)

// HostInfo captures the runtime environment at the moment `nflow self
// test` starts. Every field is populated without spawning a subprocess
// and without CGo, so detection costs microseconds and behaves
// identically on Windows, Linux, macOS, and inside containers.
type HostInfo struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	GoVersion  string `json:"go_version"`
	NumCPU     int    `json:"num_cpu"`
	CPUModel   string `json:"cpu_model,omitempty"`
	Goroutines int    `json:"goroutines"`
}

func DetectHost() HostInfo {
	return HostInfo{
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		GoVersion:  runtime.Version(),
		NumCPU:     runtime.NumCPU(),
		CPUModel:   detectCPUModel(),
		Goroutines: runtime.NumGoroutine(),
	}
}

// FormatBanner returns the one-line host summary shown above the report.
func (h HostInfo) FormatBanner() string {
	model := h.CPUModel
	if model == "" {
		model = "unknown CPU"
	}
	return fmt.Sprintf("%s/%s · %d CPUs · %s · %s",
		h.GOOS, h.GOARCH, h.NumCPU, h.GoVersion, model)
}
