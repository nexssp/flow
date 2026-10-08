package decide

import (
	"os"
	"time"

	"github.com/nexssp/kernel/xerr"
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
		return xerr.NotFound("decide: read config " + path + ": " + err.Error())
	}

	var parsed FileConfig
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return xerr.Validation("decide: unmarshal " + path + ": " + err.Error())
	}

	for name, spec := range parsed.Backends {
		if spec.Kind != "http" {
			return xerr.Validation("decide: backend " + name +
				" unsupported kind " + spec.Kind + " (supported: http)")
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
