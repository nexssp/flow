package flow

import (
	"fmt"
	"os"
	"strings"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
)

type SystemAssembler struct {
	registry *MapRegistry
	compiled []action.AnyAction
}

func NewAssembler(capabilities ...action.AnyAction) *SystemAssembler {
	return &SystemAssembler{
		registry: NewRegistry(capabilities...),
		compiled: make([]action.AnyAction, 0, 16),
	}
}

func (s *SystemAssembler) Register(name string, act action.AnyAction) *SystemAssembler {
	s.registry.Register(name, act)

	return s
}

func (s *SystemAssembler) AssembleFile(path string) ([]action.AnyAction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("flow: read file %q: %w", path, err)
	}

	return s.AssembleManifest(string(data))
}

// AssembleManifest compiles a manifest of action declarations. Each
// non-empty, non-comment line is one action; blank lines and lines
// starting with '#' or '//' are ignored.
//
// Unlike CompilePipeline, this method does NOT run SanitizeDSL on the
// input. SanitizeDSL's route-header filter treats an unindented
// ":route=" line with no arrow as a whole-flow mount point, which is
// correct for a .flow file but wrong for a manifest-of-actions: every
// line here is its own declaration and route modifiers belong to the
// action on that line.
func (s *SystemAssembler) AssembleManifest(manifestDSL string) ([]action.AnyAction, error) {
	lines := strings.Split(manifestDSL, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		parser := compiler.NewParser(line)

		ast, err := parser.ParseExpression()
		if err != nil {
			return nil, fmt.Errorf("flow: assemble %q failed: %w", line, err)
		}

		builder, err := compileAST(ast, s.registry)
		if err != nil {
			return nil, fmt.Errorf("flow: assemble %q failed: %w", line, err)
		}

		s.compiled = append(s.compiled, builder.Build())
	}

	return s.compiled, nil
}

func (s *SystemAssembler) Actions() []action.AnyAction {
	return s.compiled
}
