//go:build darwin

package selftest

import "golang.org/x/sys/unix"

func detectCPUModel() string {
	value, err := unix.Sysctl("machdep.cpu.brand_string")
	if err != nil {
		return ""
	}
	return value
}
