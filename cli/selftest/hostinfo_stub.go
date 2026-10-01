//go:build !linux && !darwin && !windows

package selftest

func detectCPUModel() string { return "" }
