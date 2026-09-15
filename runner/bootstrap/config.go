package bootstrap

import (
	"fmt"
	"os"
)

func ResolveConfigPath(flag, envKey string, defaults ...string) (string, error) {
	if flag != "" {
		if _, err := os.Stat(flag); err != nil {
			return "", fmt.Errorf("config file from flag not found: %s", flag)
		}

		return flag, nil
	}

	if envKey != "" {
		if v := os.Getenv(envKey); v != "" {
			// envKey names a trusted local env var; the value is a path
			// to a config file this process is meant to read. It is not
			// request input. Gosec G703 cannot distinguish the two.
			//nolint:gosec // G703: trusted local env, not user input
			if _, err := os.Stat(v); err != nil {
				return "", fmt.Errorf("%s=%s not found", envKey, v)
			}

			return v, nil
		}
	}

	for _, def := range defaults {
		if _, err := os.Stat(def); err == nil {
			return def, nil
		}
	}

	return "", fmt.Errorf("no config file found (checked: %v)", defaults)
}
