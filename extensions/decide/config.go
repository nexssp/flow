package decide

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// FileConfig is the YAML shape accepted by LoadConfigFile.
type FileConfig struct {
	Backends map[string]struct {
		Kind     string            `yaml:"kind"`
		Endpoint string            `yaml:"endpoint"`
		Timeout  time.Duration     `yaml:"timeout"`
		Headers  map[string]string `yaml:"headers,omitempty"`
	} `yaml:"backends"`
}

// LoadConfigFile reads a YAML config and registers every declared HTTP
// backend into the process-wide registry.
func LoadConfigFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("decide: read config %s: %w", path, err)
	}

	var parsed FileConfig
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("decide: unmarshal %s: %w", path, err)
	}

	for name, spec := range parsed.Backends {
		if spec.Kind != "http" {
			return fmt.Errorf("decide: backend %q unsupported kind %q (supported: http)", name, spec.Kind)
		}
		backend, err := NewHTTPBackend(HTTPConfig{
			Name:     name,
			Endpoint: spec.Endpoint,
			Timeout:  spec.Timeout,
			Headers:  spec.Headers,
		})
		if err != nil {
			return err
		}
		if err := Register(backend); err != nil {
			return err
		}
	}
	return nil
}
