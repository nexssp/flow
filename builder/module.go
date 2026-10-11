package builder

import (
	"os"
	"path/filepath"
	"strings"
)

type moduleRequirement struct {
	path    string
	version string
}

// FlowModuleRoot locates the Nexss Flow source module using the running
// executable or current working directory. It is shared by the CLI and the
// builder so both use the same module-root detection logic.
func FlowModuleRoot() (string, error) {
	return selfModuleRoot()
}

func readModuleRequirements(path string) ([]moduleRequirement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var requirements []moduleRequirement
	inBlock := false
	for rawLine := range strings.SplitSeq(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "//", 2)[0])
		if line == "" {
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			requirements = appendRequirement(requirements, line)
			continue
		}
		if line == "require (" {
			inBlock = true
			continue
		}
		if strings.HasPrefix(line, "require ") {
			requirements = appendRequirement(requirements, strings.TrimSpace(strings.TrimPrefix(line, "require")))
		}
	}
	return requirements, nil
}

func appendRequirement(requirements []moduleRequirement, line string) []moduleRequirement {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return requirements
	}
	return append(requirements, moduleRequirement{path: fields[0], version: fields[1]})
}

func findModuleRoot(dir string) string {
	curr := dir
	for {
		if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			return ""
		}
		curr = parent
	}
}
