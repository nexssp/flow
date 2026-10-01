//go:build windows

package selftest

import "golang.org/x/sys/windows/registry"

func detectCPUModel() string {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}
	defer func() { _ = key.Close() }()

	value, _, err := key.GetStringValue("ProcessorNameString")
	if err != nil {
		return ""
	}
	return value
}
