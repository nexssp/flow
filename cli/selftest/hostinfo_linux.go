//go:build linux

package selftest

import (
	"bufio"
	"os"
	"strings"
)

func detectCPUModel() string {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "model name") {
			continue
		}
		if _, value, ok := strings.Cut(line, ":"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
